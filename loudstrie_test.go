package loudstrie

import (
	"testing"
)

func TestBuild(t *testing.T) {
	builder := NewTrieBuilder()
	keyList := []string{
		"bbc",
		"able",
		"abc",
		"abcde",
		"can",
	}

	trie, err := builder.Build(keyList, true)
	if err != nil {
		t.Error("Build error")
	}
	if trie.GetNumOfKeys() != uint64(len(keyList)) {
		t.Error("")
	}

	trie, err = builder.Build(keyList, false)
	if err != nil {
		t.Error("Build error")
	}
	if trie.GetNumOfKeys() != uint64(len(keyList)) {
		t.Error("")
	}
}

func TestExactMatchSearch(t *testing.T) {
	builder := NewTrieBuilder()
	keyList := []string{
		"bbc",
		"able",
		"abc",
		"abcde",
		"can",
	}

	trie1, _ := builder.Build(keyList, true)
	trie2, _ := builder.Build(keyList, false)
	tries := []*Trie{&trie1, &trie2}

	for _, trie := range tries {

		for _, key := range keyList {
			id, found := (*trie).ExactMatchSearch(key)
			if !found {
				t.Error("Not found", key)
			}
			decode, found := (*trie).DecodeKey(id)
			if !found {
				t.Error("Not found", id)
			}
			if key != decode {
				t.Error("Expected", key, "got", decode)
			}
		}
		id, found := (*trie).ExactMatchSearch("NotExist")
		if found {
			t.Error("Search error for key that does not exist in the trie.", id)
		}
	}
}

func TestCommonPrefixSearch(t *testing.T) {
	builder := NewTrieBuilder()
	keyList := []string{
		"bbc",
		"able",
		"abc",
		"abcde",
		"can",
	}

	trie1, _ := builder.Build(keyList, true)
	trie2, _ := builder.Build(keyList, false)
	tries := []*Trie{&trie1, &trie2}

	for _, trie := range tries {

		results := (*trie).CommonPrefixSearch("abcde", 0)
		if len(results) != 2 {
			t.Error(results)
		}
		str, _ := (*trie).DecodeKey(results[0].ID)
		if str != "abc" || uint64(len(str)) != results[0].Length {
			t.Error(str)
		}
		str, _ = (*trie).DecodeKey(results[1].ID)
		if str != "abcde" || uint64(len(str)) != results[1].Length {
			t.Error(str)
		}

		results = (*trie).CommonPrefixSearch("abcde", 1)
		if len(results) != 1 {
			t.Error(results)
		}
	}
}

func TestPredictiveSearch(t *testing.T) {
	builder := NewTrieBuilder()
	keyList := []string{
		"bbc",
		"able",
		"abc",
		"abcde",
		"can",
	}
	trie1, _ := builder.Build(keyList, true)
	trie2, _ := builder.Build(keyList, false)
	tries := []*Trie{&trie1, &trie2}

	for _, trie := range tries {
		results := (*trie).PredictiveSearch("ab", 0)
		if len(results) != 3 {
			t.Error(results)
		}

		results = (*trie).PredictiveSearch("ab", 1)
		if len(results) != 1 {
			t.Error(results)
		}
		results = (*trie).PredictiveSearch("can", 0)
		if len(results) != 1 {
			t.Error(results)
		}
		results = (*trie).PredictiveSearch("d", 0)
		if len(results) != 0 {
			t.Error(results)
		}
		results = (*trie).PredictiveSearch("cas", 0)
		if len(results) != 0 {
			t.Error(results)
		}
	}
}

func TestDecodeKey(t *testing.T) {
    builder := NewTrieBuilder()
    keyList := []string{
        "bbc",
        "able",
        "abc",
        "abcde",
        "canon",
    }
    trie1, _ := builder.Build(keyList, true)
    trie2, _ := builder.Build(keyList, false)
    tries := []*Trie{&trie1, &trie2}

    for _, trie := range tries {
		for i := 0; i < len(keyList); i++ {
			key, found := (*trie).DecodeKey(uint64(i))
			if !found {
				t.Error("Not found", key, i);
			}
		}
		id := uint64(len(keyList)+1)
		key, found := (*trie).DecodeKey(id)
		if key != "" || found {
			t.Error("earch error for key that does not exist in the trie.", id)
		}
	}
}

func TestMarshalBinary(t *testing.T) {
	builder := NewTrieBuilder()
	keyList := []string{
		"bbc",
		"able",
		"abc",
		"abcde",
		"canon",
	}
	trie1, _ := builder.Build(keyList, true)
	trie2, _ := builder.Build(keyList, false)
	tries := []*Trie{&trie1, &trie2}

	for _, trie := range tries {
		buf, err := (*trie).MarshalBinary()
		if err != nil {
			t.Errorf(err.Error())
		}
		newtrie, err := NewTrieFromBinary(buf)
		if err != nil {
			t.Errorf(err.Error())
		}

		for _, key := range keyList {
			id, found := newtrie.ExactMatchSearch(key)
			if !found {
				t.Error("Not found", key)
			}
			decode, _ := newtrie.DecodeKey(id)
			if key != decode {
				t.Error("Expected", key, "got", decode)
			}
		}
	}
}
