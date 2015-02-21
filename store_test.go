package store

import (
	"encoding/json"
	"io"
	"sync"
	"testing"
)

var TestPath = "a9sfdsv2"

type TestElement struct {
	ElementID ElementID
	Data      string
}

func (el *TestElement) ID() ElementID {
	return el.ElementID
}

func (el *TestElement) Load(r io.Reader) error {
	var loaded TestElement
	dec := json.NewDecoder(r)
	if err := dec.Decode(&loaded); err != nil {
		return err
	}

	*el = loaded
	return nil
}

func (el *TestElement) Store(w io.Writer) error {
	enc := json.NewEncoder(w)
	if err := enc.Encode(el); err != nil {
		return err
	}

	return nil
}

var TestElems = []TestElement{
	{1, "foobar"},
	{2, "bazbaz"},
	{3, "lelel"},
}

func TestLRUInsertion(t *testing.T) {
	lru := NewLRUCache(1)
	lru.Cache(&TestElems[0])
	retrieved := lru.Get(TestElems[0].ElementID)
	if retrieved == nil || retrieved.(*TestElement).Data != TestElems[0].Data {
		t.Fatal("expected ", TestElems[0].Data, " got ", retrieved.(*TestElement).Data)
	}
}

func TestLRUEviction(t *testing.T) {
	lru := NewLRUCache(1)
	lru.Cache(&TestElems[0])
	lru.Cache(&TestElems[1])
	lru.Cache(&TestElems[2])

	retrieved := lru.Get(TestElems[0].ElementID)
	if retrieved != nil {
		t.Fatal("able to retrieve an element that should have been evicted")
	}

	retrieved = lru.Get(TestElems[1].ElementID)
	if retrieved != nil {
		t.Fatal("able to retrieve an element that should have been evicted")
	}

	retrieved = lru.Get(TestElems[2].ElementID)
	if retrieved == nil || retrieved.(*TestElement).Data != TestElems[2].Data {
		t.Fatal("expected ", TestElems[2].Data, " got ", retrieved.(*TestElement).Data)
	}
}

func TestLRUPromotion(t *testing.T) {
	lru := NewLRUCache(2)
	lru.Cache(&TestElems[0])
	lru.Cache(&TestElems[1])
	lru.Cache(&TestElems[0])
	lru.Cache(&TestElems[2])
	// lru should be (left == front) : [2, 0]

	retrieved := lru.Get(TestElems[0].ElementID)
	if retrieved == nil || retrieved.(*TestElement).Data != TestElems[0].Data {
		t.Fatal("expected ", TestElems[0].Data, " got ", retrieved.(*TestElement).Data)
	}

	retrieved = lru.Get(TestElems[1].ElementID)
	if retrieved != nil {
		t.Fatal("expected  nil, got ", retrieved.(*TestElement).Data)
	}

	retrieved = lru.Get(TestElems[2].ElementID)
	if retrieved == nil || retrieved.(*TestElement).Data != TestElems[2].Data {
		t.Fatal("expected ", TestElems[2].Data, " got ", retrieved.(*TestElement).Data)
	}

}

func TestNilCache(t *testing.T) {
	// zero size cache == nil
	// should still work
	lru := NewLRUCache(0)
	lru.Cache(&TestElems[0])
	retrieved := lru.Get(TestElems[0].ElementID)
	if retrieved != nil {
		t.Fatal("expected nil, got ", retrieved)
	}
}

func BenchmarkLRURetrieval(b *testing.B) {
	lru := NewLRUCache(1)
	lru.Cache(&TestElems[0])

	for i := 0; i < b.N; i++ {
		el := lru.Get(TestElems[0].ElementID)
		if el == nil || el.(*TestElement).Data != TestElems[0].Data {
			b.Fatal("unexpected value: ", el)
		}
	}
}

func TestStoreRetrieveNonExistant(t *testing.T) {
	s, err := New(TestPath)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Remove()

	retElem := &TestElement{ElementID: TestElems[0].ElementID}
	if _, err := s.Get(retElem); err != ErrDoesNotExist {
		t.Fatal("expected ErrDoesNotExist, got", err)
	}
}

func TestStoreDuplicateInsertion(t *testing.T) {
	s, err := New(TestPath)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Remove()

	elem := &TestElems[0]
	if err := s.Put(elem); err != nil {
		t.Fatal(err)
	}

	if err := s.Put(elem); err != ErrAlreadyExists {
		t.Fatal("expected ErrAlreadyExists, got", err)
	}

}

func TestStoreConcurrentDuplicateInsertion(t *testing.T) {
	var err1, err2 error

	s, err := New(TestPath)
	if err != nil {
		t.Fatal(err)
	}

	defer s.Remove()

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		err1 = s.Put(&TestElems[0])
		wg.Done()
	}()

	go func() {
		err2 = s.Put(&TestElems[0])
		wg.Done()
	}()

	wg.Wait()

	if !((err1 == nil && err2 == ErrAlreadyExists) ||
		(err1 == ErrAlreadyExists && err2 == nil)) {

		t.Fatal("expected (nil, ErrAlreadyExists) || (ErrAlreadyExists, nil), got", err1, err2)
	}
}

func testStorePut(t *testing.T, midSync bool, cacheMode CacheMode, cacheSize, nelems int) {
	s, err := New(TestPath)
	if err != nil {
		t.Fatal(err)
	}

	if cacheSize > 0 {
		cache := NewLRUCache(cacheSize)
		s.SetCache(cache, cacheMode)
	}

	defer s.Remove()

	lim := len(TestElems)
	if nelems < lim {
		lim = nelems
	}

	for i := 0; i < lim; i++ {
		elem := &TestElems[i]
		if err := s.Put(elem); err != nil {
			t.Fatal(err)
		}
	}

	if midSync {
		s.Sync()
	}

	for i := 0; i < lim; i++ {
		retElem := &TestElement{ElementID: TestElems[i].ElementID}
		if retElem, err := s.Get(retElem); err != nil {
			t.Fatal(err)
		} else if retElem == nil {
			t.Fatal("expected non-nil retrieved element")
		} else if retElem.(*TestElement).Data != TestElems[i].Data {
			t.Fatal("expected", TestElems[i].Data, "got", retElem.(*TestElement).Data)
		}
	}

	s.Sync()
}

func TestStorePut(t *testing.T) {
	// don't sync, don't cache, one element
	testStorePut(t, false, 0, 0, 1)
}

func TestStorePutSync(t *testing.T) {
	// sync, don't cache, one element
	testStorePut(t, true, 0, 0, 1)
}

func TestStorePutMultiple(t *testing.T) {
	// don't sync, don't cache, three elements
	testStorePut(t, false, 0, 0, 3)
}

func TestStorePutSyncMultiple(t *testing.T) {
	// sync, don't cache, three elements
	testStorePut(t, true, 0, 0, 3)
}

func TestStorePutSmallCache(t *testing.T) {
	// don't sync, cache, one element
	testStorePut(t, false, CacheOnGet|CacheOnPut, 1, 1)
}

func TestStorePutSyncSmallCache(t *testing.T) {
	// sync, cache, one element
	testStorePut(t, true, CacheOnGet|CacheOnPut, 1, 1)
}

func TestStorePutMultipleSmallCache(t *testing.T) {
	// don't sync, cache, three elements
	testStorePut(t, false, CacheOnGet|CacheOnPut, 1, 3)
}

func TestStorePutSyncMultipleSmallCache(t *testing.T) {
	// sync, cache, three elements
	testStorePut(t, true, CacheOnGet|CacheOnPut, 1, 3)
}

func TestStorePutLargeCache(t *testing.T) {
	// don't sync, cache, one element
	testStorePut(t, false, CacheOnGet|CacheOnPut, 4, 1)
}

func TestStorePutSyncLargeCache(t *testing.T) {
	// sync, cache, one element
	testStorePut(t, true, CacheOnGet|CacheOnPut, 4, 1)
}

func TestStorePutMultipleLargeCache(t *testing.T) {
	// don't sync,cache, three elements
	testStorePut(t, false, CacheOnGet|CacheOnPut, 4, 3)
}

func TestStorePutSyncMultipleLargeCache(t *testing.T) {
	// sync, cache, three elements
	testStorePut(t, true, CacheOnGet|CacheOnPut, 4, 3)
}
