package limiter

import "errors"

type node[T any] struct {
	Value  T
	CloseC <-chan error
	r      *LinkedList[T]
	prev   *node[T]
	next   *node[T]
}

func (n *node[T]) delete() {
	r := n.r
	n.r = nil
	prev := n.prev
	next := n.next
	if prev != nil {
		prev.next = next
	}
	if next != nil {
		next.prev = prev
	}
	if r.head == n {
		r.head = next
	}
	if r.tail == n {
		r.tail = prev
	}
}

type LinkedList[T any] struct {
	max_size uint64
	size     uint64
	head     *node[T]
	tail     *node[T]
}

func CreateLinkedList[T any](max uint64) *LinkedList[T] {
	return &LinkedList[T]{
		max_size: max, size: 0, head: nil, tail: nil,
	}
}

func (r *LinkedList[T]) UpdateSize(size uint64) {

	r.max_size = size
	if r.max_size <= size {
		return
	}

	x := r.tail

	for x != nil && r.size <= r.max_size {
		prev := x.prev
		x.delete()
		x = prev
	}
}

func (r *LinkedList[T]) Pop() (ans T, present bool) {
	present = false
	if r.head == nil {
		return
	}

	r.size -= 1

	oldHead := r.head
	r.head = oldHead.next
	if r.head != nil {
		r.head.prev = nil
	}
	ans = oldHead.Value
	return
}

// pop an array of size equal to the lesser of the list size or count
func (r *LinkedList[T]) PopArray(count uint64) []T {
	size := r.size
	if count < size {
		size = count
	}

	ans := make([]T, size)
	var present bool
	for i := uint64(0); i < size; i++ {
		ans[i], present = r.Pop()
		if !present {
			panic("element should be present")
		}
	}
	return ans
}

func (r *LinkedList[T]) Append(value T) <-chan error {
	signalC := make(chan error, 1)
	if r.max_size <= r.size {
		signalC <- errors.New("buffer is full")
		return signalC
	}
	r.size++

	n := &node[T]{
		Value: value,
		r:     r,
	}

	if r.head == nil {
		r.head = n
		r.tail = n
	} else {
		oldTail := r.tail
		r.tail = n
		oldTail.next = n
		n.prev = oldTail
	}

	return signalC
}
