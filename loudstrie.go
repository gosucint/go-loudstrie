/*
Package loudstrie is implementation of LOUDS(Level-Order Unary Degree Sequence) Trie.

Synopsis
	import (
			"fmt"

			"github.com/hideo55/go-loudstrie"
	)

	func example() {
		builder := loudstrie.NewTrieBuilder()
		keyList := []string{
			"bbc",
			"able",
			"abc",
			"abcde",
			"can",
		}
		trie, err := builder.Build(keyList, false)

		result := trie.CommonPrefixSearch("ab", 0)
		for _, item := range result {
			// item has two menbers, ID and Depth.
			// ID: ID of the leaf node.
			// Depth: Tree depth of the leaf node.
			key := trie.DecodeKey(item.ID)
			fmt.Printf("ID:%d, key:%s, len:%d\n", item.ID, key, item.Depth)
		}
	}
*/
package loudstrie

import (
	"bytes"
	"encoding"
	"encoding/binary"
	"errors"
	"unsafe"

	"github.com/hideo55/go-sbvector"
)

/*
TrieData holds information of LOUDS Trie.
*/
type TrieData struct {
	louds       sbvector.SuccinctBitVector
	terminal    sbvector.SuccinctBitVector
	tail        sbvector.SuccinctBitVector
	vtails      []string
	edges       []byte
	numOfKeys   uint64
	hasTailTrie bool
	tailTrie    Trie
	tailIDs     sbvector.SuccinctBitVector
	tailIDSize  uint64
}

/*
Result holds result of common-prefix search.
*/
type Result struct {
	ID    uint64
	Depth uint64
}

/*
Trie is interface of LOUDS Trie.
*/
type Trie interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
	ExactMatchSearch(key string) uint64
	CommonPrefixSearch(key string, limit uint64) []Result
	PredictiveSearch(key string, limit uint64) []uint64
	Traverse(key string, keyLen uint64, nodePos *uint64, zeros *uint64, keyPos *uint64) uint64
	DecodeKey(id uint64) string
	GetNumOfKeys() uint64
}

const (
	// NotFound indicates `value is not found`
	NotFound uint64 = 0xFFFFFFFFFFFFFFFF
	// CanNotTraverse indicates `subsequent node is not found`
	CanNotTraverse uint64 = 0xFFFFFFFFFFFFFFFE
	// NoLimit indicates `Doesn't limit number of results`
	NoLimit uint64 = 0xFFFFFFFFFFFFFFFF

	sizeOfInt32 uint32 = 4
	sizeOfInt64 uint32 = 8
)

var (
	// ErrorInvalidFormat indicates that binary format is invalid.
	ErrorInvalidFormat = errors.New("UnmarshalBinary: invalid binary format")
)

/*
ExactMatchSearch looks up key exact match with query string.
*/
func (trie *TrieData) ExactMatchSearch(key string) uint64 {
	id := uint64(0)
	nodePos := uint64(0)
	zeros := uint64(0)
	keyPos := uint64(0)
	keyLen := uint64(len(key))
	for keyPos <= keyLen {
		id = trie.Traverse(key, keyLen, &nodePos, &zeros, &keyPos)
		if id == CanNotTraverse {
			break
		}
		if keyPos == keyLen+1 {
			return id
		}
	}
	return NotFound
}

/*
CommonPrefixSearch looks up keys from the possible prefixes of a query string.
*/
func (trie *TrieData) CommonPrefixSearch(key string, limit uint64) []Result {
	nodePos := uint64(0)
	zeros := uint64(0)
	keyPos := uint64(0)
	keyLen := uint64(len(key))
	res := make([]Result, 0)
	if limit == 0 {
		limit = NoLimit
	}

	for {
		id := trie.Traverse(key, keyLen, &nodePos, &zeros, &keyPos)
		if id == CanNotTraverse {
			break
		}
		if id != NotFound {
			res = append(res, Result{id, keyPos - 1})
			if uint64(len(res)) == limit {
				break
			}
		}
	}
	return res
}

/*
PredictiveSearch searches keys starting with a query string.
*/
func (trie *TrieData) PredictiveSearch(key string, limit uint64) []uint64 {
	res := make([]uint64, 0)
	if limit == 0 {
		limit = NoLimit
	}
	pos := uint64(2)
	zeros := uint64(2)
	keyLen := uint64(len(key))
	for i := uint64(0); i < keyLen; i++ {
		ones := pos - zeros
		if ok, _ := trie.tail.Get(ones); ok {
			tailID, _ := trie.tail.Rank1(ones)
			tail := trie.getTail(tailID)
			for j := i; j < keyLen; j++ {
				if key[j] != tail[j-i] {
					return res
				}
			}
			id, _ := trie.terminal.Rank1(ones)
			res = append(res, id)
			return res
		}
		trie.getChild(key[i], &pos, &zeros)
		if pos == NotFound {
			return res
		}
	}
	trie.enumerateAll(pos, zeros, &res, limit)
	return res
}

/*
Traverse the node of the trie.
*/
func (trie *TrieData) Traverse(key string, keyLen uint64, nodePos *uint64, zeros *uint64, keyPos *uint64) uint64 {
	id := NotFound
	if *nodePos == NotFound {
		return CanNotTraverse
	}
	defaultPos := uint64(2)
	*nodePos = max(*nodePos, defaultPos)
	*zeros = max(*zeros, defaultPos)
	ones := *nodePos - *zeros
	hasTail, _ := trie.tail.Get(ones)
	if hasTail {
		retLen := uint64(0)
		tailRank, _ := trie.tail.Rank1(ones)
		if trie.tailMatch(key, keyLen, *keyPos, tailRank, &retLen) {
			*keyPos += retLen
			id, _ = trie.terminal.Rank1(ones)
		}
	} else if ok, _ := trie.terminal.Get(ones); ok {
		id, _ = trie.terminal.Rank1(ones)
	}

	if *keyPos < keyLen {
		trie.getChild(key[*keyPos], nodePos, zeros)
	} else {
		*nodePos = NotFound
	}

	*keyPos++
	if id == NotFound && *nodePos == NotFound {
		return CanNotTraverse
	}

	return id
}

func (trie *TrieData) isLeaf(pos uint64) bool {
	val, _ := trie.louds.Get(pos)
	return val
}

func (trie *TrieData) getParent(c *byte, pos *uint64, zeros *uint64) {
	*zeros = *pos - *zeros + uint64(1)
	*pos, _ = trie.louds.Select0(*zeros - uint64(1))
	if *zeros < uint64(2) {
		return
	}
	*c = trie.edges[*zeros-uint64(2)]
}

func (trie *TrieData) getChild(c byte, pos *uint64, zeros *uint64) {
	for {
		if trie.isLeaf(*pos) {
			*pos = NotFound
			break
		}
		if c == trie.edges[*zeros-uint64(2)] {
			*pos, _ = trie.louds.Select1(*zeros - uint64(1))
			*pos++
			*zeros = *pos - *zeros + uint64(1)
			break
		}
		*pos++
		*zeros++
	}
}

func (trie *TrieData) enumerateAll(pos uint64, zeros uint64, res *[]uint64, limit uint64) {
	ones := pos - zeros
	term, _ := trie.terminal.Get(ones)
	if term {
		rank, _ := trie.terminal.Rank1(ones)
		*res = append(*res, rank)
	}
	for i := uint64(0); uint64(len(*res)) < limit; i++ {
		if ok, _ := trie.louds.Get(pos + i); ok {
			break
		}
		nextPos, _ := trie.louds.Select1(zeros + i - uint64(1))
		nextPos++
		trie.enumerateAll(nextPos, nextPos-zeros-i+uint64(1), res, limit)
	}
}

func (trie *TrieData) tailMatch(str string, strlen uint64, depth uint64, tailID uint64, retLen *uint64) bool {
	tail := trie.getTail(tailID)
	tailLen := uint64(len(tail))
	if tailLen > (strlen - depth) {
		return false
	}
	for i := uint64(0); i < tailLen; i++ {
		if str[i+depth] != tail[i] {
			return false
		}
	}
	*retLen = tailLen
	return true
}

func (trie *TrieData) getTail(tailID uint64) string {
	if trie.hasTailTrie {
		id, _ := trie.tailIDs.GetBits(trie.tailIDSize*tailID, trie.tailIDSize)
		tail := trie.tailTrie.DecodeKey(id)
		runes := []rune(tail)
		for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
			runes[i], runes[j] = runes[j], runes[i]
		}
		return string(runes)
	}
	return trie.vtails[tailID]
}

/*
DecodeKey returns key string corresponding to the ID.
*/
func (trie *TrieData) DecodeKey(id uint64) string {
	nodeID, _ := trie.terminal.Select1(id)
	pos, _ := trie.louds.Select1(nodeID)
	pos++
	zeros := pos - nodeID
	var keyBuf []byte
	for {
		c := byte(0)
		trie.getParent(&c, &pos, &zeros)
		if pos == 0 {
			break
		}
		keyBuf = append([]byte{c}, keyBuf...)
	}
	key := bytes.NewBuffer(keyBuf).String()
	hasTail, _ := trie.tail.Get(nodeID)
	if hasTail {
		rank, _ := trie.tail.Rank1(nodeID)
		tailStr := trie.getTail(rank)
		key += tailStr
	}

	return key
}

/*
GetNumOfKeys returns number of keys in trie.
*/
func (trie *TrieData) GetNumOfKeys() uint64 {
	return trie.numOfKeys
}

/*
MarshalBinary implements the encoding.BinaryMarshaler interface.
*/
func (trie *TrieData) MarshalBinary() ([]byte, error) {
	buffer := new(bytes.Buffer)

	binary.Write(buffer, binary.LittleEndian, &trie.numOfKeys)

	// louds
	buf, _ := trie.louds.MarshalBinary()
	loudsSize := uint32(len(buf))
	binary.Write(buffer, binary.LittleEndian, &loudsSize)
	binary.Write(buffer, binary.LittleEndian, buf)

	// terminal
	buf, _ = trie.terminal.MarshalBinary()
	terminalSize := uint32(len(buf))
	binary.Write(buffer, binary.LittleEndian, &terminalSize)
	binary.Write(buffer, binary.LittleEndian, buf)

	// tail
	buf, _ = trie.tail.MarshalBinary()
	tailSize := uint32(len(buf))
	binary.Write(buffer, binary.LittleEndian, &tailSize)
	binary.Write(buffer, binary.LittleEndian, buf)

	// edges
	edgesSize := uint32(len(trie.edges))
	binary.Write(buffer, binary.LittleEndian, &edgesSize)
	binary.Write(buffer, binary.LittleEndian, trie.edges)

	// hasTailTrie
	hasTailTrie := uint32(0)
	if trie.hasTailTrie {
		hasTailTrie = uint32(1)
	}
	binary.Write(buffer, binary.LittleEndian, &hasTailTrie)

	if trie.hasTailTrie {
		// tailTrie
		buf, _ = trie.tailTrie.MarshalBinary()
		tailTrieSize := uint32(len(buf))
		binary.Write(buffer, binary.LittleEndian, &tailTrieSize)
		binary.Write(buffer, binary.LittleEndian, buf)

		// tailIDSize
		binary.Write(buffer, binary.LittleEndian, &trie.tailIDSize)

		// tailIDs
		buf, _ = trie.tailIDs.MarshalBinary()
		tailIDsSize := uint32(len(buf))
		binary.Write(buffer, binary.LittleEndian, &tailIDsSize)
		binary.Write(buffer, binary.LittleEndian, buf)

	} else {
		vtailSize := uint32(len(trie.vtails))
		binary.Write(buffer, binary.LittleEndian, &vtailSize)
		for _, str := range trie.vtails {
			buf = *(*[]byte)(unsafe.Pointer(&str))
			strlen := uint32(len(buf))
			binary.Write(buffer, binary.LittleEndian, &strlen)
			binary.Write(buffer, binary.LittleEndian, buf)
		}
	}
	return buffer.Bytes(), nil
}

/*
UnmarshalBinary implements the encoding.BinaryUnmarshaler interface.
*/
func (trie *TrieData) UnmarshalBinary(data []byte) error {
	newtrie := new(TrieData)
	offset := uint32(0)
	buf := data[offset : offset+sizeOfInt64]
	if uint32(len(buf)) < sizeOfInt64 {
		return ErrorInvalidFormat
	}
	offset += sizeOfInt64
	newtrie.numOfKeys = binary.LittleEndian.Uint64(buf)
	buf = data[offset : offset+sizeOfInt32]
	if uint32(len(buf)) < sizeOfInt32 {
		return ErrorInvalidFormat
	}
	offset += sizeOfInt32
	loudsSize := binary.LittleEndian.Uint32(buf)

	buf = data[offset : offset+loudsSize]
	if uint32(len(buf)) < loudsSize {
		return ErrorInvalidFormat
	}
	louds, err := sbvector.NewVectorFromBinary(buf)
	if err != nil {
		return ErrorInvalidFormat
	}
	newtrie.louds = louds
	offset += loudsSize

	buf = data[offset : offset+sizeOfInt32]
	if uint32(len(buf)) < sizeOfInt32 {
		return ErrorInvalidFormat
	}
	offset += sizeOfInt32
	terminalSize := binary.LittleEndian.Uint32(buf)

	buf = data[offset : offset+terminalSize]
	if uint32(len(buf)) < terminalSize {
		return ErrorInvalidFormat
	}
	terminal, err := sbvector.NewVectorFromBinary(buf)
	if err != nil {
		return ErrorInvalidFormat
	}
	newtrie.terminal = terminal
	offset += terminalSize

	buf = data[offset : offset+sizeOfInt32]
	if uint32(len(buf)) < sizeOfInt32 {
		return ErrorInvalidFormat
	}
	offset += sizeOfInt32
	tailSize := binary.LittleEndian.Uint32(buf)

	buf = data[offset : offset+tailSize]
	if uint32(len(buf)) < tailSize {
		return ErrorInvalidFormat
	}
	tail, err := sbvector.NewVectorFromBinary(buf)
	if err != nil {
		return ErrorInvalidFormat
	}
	newtrie.tail = tail
	offset += tailSize

	buf = data[offset : offset+sizeOfInt32]
	if uint32(len(buf)) < sizeOfInt32 {
		return ErrorInvalidFormat
	}
	offset += sizeOfInt32
	edgesSize := binary.LittleEndian.Uint32(buf)

	buf = data[offset : offset+edgesSize]
	if uint32(len(buf)) < edgesSize {
		return ErrorInvalidFormat
	}
	newtrie.edges = make([]byte, edgesSize)
	copy(newtrie.edges, buf)
	offset += edgesSize

	buf = data[offset : offset+sizeOfInt32]
	if uint32(len(buf)) < sizeOfInt32 {
		return ErrorInvalidFormat
	}
	offset += sizeOfInt32
	hasTailTrie := binary.LittleEndian.Uint32(buf)
	if hasTailTrie == 0 {
		newtrie.hasTailTrie = false
	} else {
		newtrie.hasTailTrie = true
	}

	if newtrie.hasTailTrie {
		buf = data[offset : offset+sizeOfInt32]
		if uint32(len(buf)) < sizeOfInt32 {
			return ErrorInvalidFormat
		}
		offset += sizeOfInt32
		tailTrieSize := binary.LittleEndian.Uint32(buf)

		buf = data[offset : offset+tailTrieSize]
		if uint32(len(buf)) < tailTrieSize {
			return ErrorInvalidFormat
		}
		newtrie.tailTrie = &TrieData{}
		err = newtrie.tailTrie.UnmarshalBinary(buf)
		if err != nil {
			return ErrorInvalidFormat
		}
		offset += tailTrieSize

		buf = data[offset : offset+sizeOfInt64]
		if uint32(len(buf)) < sizeOfInt64 {
			return ErrorInvalidFormat
		}
		offset += sizeOfInt64
		newtrie.tailIDSize = binary.LittleEndian.Uint64(buf)

		buf = data[offset : offset+sizeOfInt32]
		if uint32(len(buf)) < sizeOfInt32 {
			return ErrorInvalidFormat
		}
		offset += sizeOfInt32
		tailIDsSize := binary.LittleEndian.Uint32(buf)

		buf = data[offset : offset+tailIDsSize]
		if uint32(len(buf)) < tailIDsSize {
			return ErrorInvalidFormat
		}
		tailIDs, err := sbvector.NewVectorFromBinary(buf)
		if err != nil {
			return ErrorInvalidFormat
		}
		newtrie.tailIDs = tailIDs
	} else {
		buf = data[offset : offset+sizeOfInt32]
		if uint32(len(buf)) < sizeOfInt32 {
			return ErrorInvalidFormat
		}
		offset += sizeOfInt32
		vtailSize := binary.LittleEndian.Uint32(buf)
		newtrie.vtails = make([]string, vtailSize)
		for i := uint32(0); i < vtailSize; i++ {
			buf = data[offset : offset+sizeOfInt32]
			if uint32(len(buf)) < sizeOfInt32 {
				return ErrorInvalidFormat
			}
			offset += sizeOfInt32
			strSize := binary.LittleEndian.Uint32(buf)

			buf = data[offset : offset+strSize]
			if uint32(len(buf)) < strSize {
				return ErrorInvalidFormat
			}
			offset += strSize
			newtrie.vtails[i] = bytes.NewBuffer(buf).String()
		}
	}

	trie.numOfKeys = newtrie.numOfKeys
	trie.louds = newtrie.louds
	trie.terminal = newtrie.terminal
	trie.tail = newtrie.tail
	trie.edges = make([]byte, edgesSize)
	copy(trie.edges, newtrie.edges)
	if newtrie.hasTailTrie {
		trie.hasTailTrie = true
		trie.tailTrie = newtrie.tailTrie
		trie.tailIDSize = newtrie.tailIDSize
		trie.tailIDs = newtrie.tailIDs
	} else {
		trie.hasTailTrie = false
		trie.vtails = make([]string, len(newtrie.vtails))
		trie.vtails = newtrie.vtails
	}
	return nil
}

func max(x uint64, y uint64) uint64 {
	if x < y {
		return y
	}
	return x
}
