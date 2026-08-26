package domain

import (
	"errors"
	"math/big"
	"testing"
)

type allocationModelNode struct {
	parent                     int
	grant, available, consumed int64
	allocatedToOpenChildren    int64
	open                       bool
}

func FuzzResourceAllocationConservation(f *testing.F) {
	f.Add(int64(100), []byte{0, 40, 0, 20, 1, 7, 2, 0, 1, 9, 2})
	f.Add(int64(1), []byte{0, 1, 1, 1, 2})
	f.Add(int64(1_000_000), []byte{0, 255, 0, 127, 1, 63, 0, 31, 2, 1, 2})

	f.Fuzz(func(t *testing.T, rawGrant int64, actions []byte) {
		grant := rawGrant % 1_000_001
		if grant < 0 {
			grant = -grant
		}
		if grant == 0 {
			grant = 1
		}
		nodes := []allocationModelNode{{parent: -1, grant: grant, available: grant, open: true}}

		for offset := 0; offset+2 < len(actions); offset += 3 {
			index := int(actions[offset]) % len(nodes)
			node := &nodes[index]
			switch actions[offset+1] % 3 {
			case 0: // reserve a child from current availability
				if !node.open || node.available == 0 {
					break
				}
				amount := int64(actions[offset+2])%node.available + 1
				node.available -= amount
				node.allocatedToOpenChildren += amount
				nodes = append(nodes, allocationModelNode{parent: index, grant: amount, available: amount, open: true})
			case 1: // apply trusted, nonnegative usage
				if !node.open || node.available == 0 {
					break
				}
				amount := int64(actions[offset+2]) % (node.available + 1)
				node.available -= amount
				node.consumed += amount
			case 2: // settle only an open leaf, returning unused capacity
				if index == 0 || !node.open || node.allocatedToOpenChildren != 0 {
					break
				}
				parent := &nodes[node.parent]
				parent.available += node.available
				parent.consumed += node.consumed
				parent.allocatedToOpenChildren -= node.grant
				node.open = false
			}
			assertAllocationModel(t, nodes)
		}
	})
}

func assertAllocationModel(t *testing.T, nodes []allocationModelNode) {
	t.Helper()
	for index, node := range nodes {
		if !node.open {
			continue
		}
		if node.available < 0 || node.consumed < 0 || node.allocatedToOpenChildren < 0 {
			t.Fatalf("node %d has a negative balance: %+v", index, node)
		}
		if got := node.available + node.consumed + node.allocatedToOpenChildren; got != node.grant {
			t.Fatalf("node %d violates conservation: available(%d) + consumed(%d) + allocated(%d) = %d, grant=%d", index, node.available, node.consumed, node.allocatedToOpenChildren, got, node.grant)
		}
	}
}

func FuzzCheckedDecimalArithmeticBounds(f *testing.F) {
	f.Add(int64(0), int64(0), uint8(0))
	f.Add(int64(^uint64(0)>>1), int64(1), uint8(MaxDecimalScale))
	f.Add(-int64(^uint64(0)>>1)-1, int64(-1), uint8(1))

	f.Fuzz(func(t *testing.T, left, right int64, rawScale uint8) {
		scale := rawScale % (MaxDecimalScale + 1)
		a, _ := NewDecimal(left, scale)
		b, _ := NewDecimal(right, scale)
		sum, sumErr := a.Add(b)
		checkDecimalOperation(t, "add", left, right, new(big.Int).Add(big.NewInt(left), big.NewInt(right)), sum, sumErr)
		difference, differenceErr := a.Sub(b)
		checkDecimalOperation(t, "subtract", left, right, new(big.Int).Sub(big.NewInt(left), big.NewInt(right)), difference, differenceErr)
	})
}

func checkDecimalOperation(t *testing.T, operation string, left, right int64, want *big.Int, got Decimal, err error) {
	t.Helper()
	fits := want.IsInt64()
	if !fits {
		if !errors.Is(err, ErrDecimalOverflow) {
			t.Fatalf("%s %d and %d: expected overflow, got %v", operation, left, right, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("%s %d and %d: unexpected error %v", operation, left, right, err)
	}
	if got.Coefficient() != want.Int64() {
		t.Fatalf("%s %d and %d: got %d, want %d", operation, left, right, got.Coefficient(), want.Int64())
	}
}
