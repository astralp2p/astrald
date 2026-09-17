package fs

import (
	"errors"
	"fmt"
	"slices"
	"testing"
)

// note: process receives the collector's own buffer, so the recorder keeps copies.
func recordBatches(calls *[][]int, err error) func([]int) error {
	return func(batch []int) error {
		*calls = append(*calls, slices.Clone(batch))
		return err
	}
}

func TestBatchCollector_AddFlushesFullBatches(t *testing.T) {
	var calls [][]int
	c := NewBatchCollector(3, recordBatches(&calls, nil))

	for i := 1; i <= 7; i++ {
		if err := c.Add(i); err != nil {
			t.Fatalf("Add(%d): %v", i, err)
		}
	}
	if got, want := fmt.Sprint(calls), "[[1 2 3] [4 5 6]]"; got != want {
		t.Fatalf("batches after Add 1..7: got %s, want %s", got, want)
	}

	if err := c.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got, want := fmt.Sprint(calls), "[[1 2 3] [4 5 6] [7]]"; got != want {
		t.Fatalf("batches after Flush: got %s, want %s", got, want)
	}
}

func TestBatchCollector_FlushEmpty(t *testing.T) {
	var calls [][]int
	c := NewBatchCollector(3, recordBatches(&calls, nil))

	if err := c.Flush(); err != nil {
		t.Fatalf("Flush: got %v, want nil", err)
	}
	if len(calls) != 0 {
		t.Fatalf("process calls on an empty Flush: got %v, want none", calls)
	}
}

func TestBatchCollector_ProcessErrorDropsBatch(t *testing.T) {
	errX := errors.New("x")
	var calls [][]int
	c := NewBatchCollector(2, recordBatches(&calls, errX))

	if err := c.Add(1); err != nil {
		t.Fatalf("Add(1): %v", err)
	}
	if err := c.Add(2); !errors.Is(err, errX) {
		t.Fatalf("Add(2) filling the batch: got %v, want %v", err, errX)
	}

	if err := c.Flush(); err != nil {
		t.Fatalf("Flush after the failed batch: got %v, want nil", err)
	}
	if got, want := fmt.Sprint(calls), "[[1 2]]"; got != want {
		t.Fatalf("batches: got %s, want %s", got, want)
	}
}

func TestBatchCollector_Iter(t *testing.T) {
	var calls [][]int
	c := NewBatchCollector(2, recordBatches(&calls, nil))

	err := c.Iter(func(yield func(int) error) error {
		for i := 1; i <= 4; i++ {
			if err := yield(i); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Iter: %v", err)
	}
	if got, want := fmt.Sprint(calls), "[[1 2] [3 4]]"; got != want {
		t.Fatalf("batches: got %s, want %s", got, want)
	}
}

func TestBatchCollector_IterError(t *testing.T) {
	errY := errors.New("y")
	var calls [][]int
	c := NewBatchCollector(2, recordBatches(&calls, nil))

	err := c.Iter(func(yield func(int) error) error {
		if err := yield(1); err != nil {
			return err
		}
		return errY
	})
	if !errors.Is(err, errY) {
		t.Fatalf("Iter: got %v, want %v", err, errY)
	}
	if len(calls) != 0 {
		t.Fatalf("process calls: got %v, want none", calls)
	}
}
