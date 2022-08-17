package list

type Generic[T any] struct {
	Size uint32
	head *genericNode[T]
	tail *genericNode[T]
}

type genericNode[T any] struct {
	next  *genericNode[T]
	prev  *genericNode[T]
	value T
}

func CreateGeneric[T any]() *Generic[T] {
	g := new(Generic[T])
	g.Size = 0
	g.head = nil
	g.tail = nil
	return g
}

// attach obj to the end of a linked list
func (g *Generic[T]) Append(obj T) {
	g.Size++
	node := &genericNode[T]{next: nil, prev: nil, value: obj}
	if g.tail == nil {
		g.head = node
		g.tail = node
	} else {
		oldTail := g.tail
		node.prev = oldTail
		oldTail.next = node
		g.tail = node
	}
}

func (g *Generic[T]) Head() (ans T, is_present bool) {
	if g.head == nil {
		is_present = false
	} else {
		is_present = true
		ans = g.head.value
	}
	return
}

// remove and return the first element of the linked list
func (g *Generic[T]) Pop() (ans T, is_present bool) {

	head := g.head
	if head == nil {
		is_present = false
	} else {
		ans = head.value
		g.remove(head)
		is_present = true
	}

	return
}

func (g *Generic[T]) Tail() (ans T, is_present bool) {
	if g.tail == nil {
		is_present = false
	} else {
		is_present = true
		ans = g.tail.value
	}
	return
}

func (g *Generic[T]) Iterate(callback func(obj T, index uint32, delete func()) error) error {
	var i uint32 = 0
	var err error
	for node := g.head; node != nil; node = node.next {
		err = callback(node.value, i, func() { g.remove(node) })
		if err != nil {
			return err
		}
		i++
	}
	return nil
}

func (g *Generic[T]) IterateReverse(callback func(obj T, index uint32, delete func()) error) error {
	var i uint32 = g.Size - 1
	var err error
	for node := g.tail; node != nil; node = node.prev {
		err = callback(node.value, i, func() {
			g.remove(node)
		})
		if err != nil {
			return err
		}
		i--
	}
	return nil
}

func (g *Generic[T]) Array() []T {
	ans := make([]T, g.Size)
	g.Iterate(func(obj T, index uint32, delete func()) error {
		ans[index] = obj
		return nil
	})
	return ans
}

func (g *Generic[T]) remove(node *genericNode[T]) {
	if node == nil {
		return
	}
	prevNode := node.prev
	nextNode := node.next

	g.Size = g.Size - 1

	// sort out links
	if prevNode == nil && nextNode == nil {
		g.head = nil
		g.tail = nil
	} else if prevNode == nil {
		g.head = nextNode
		nextNode.prev = nil
	} else if nextNode == nil {
		g.tail = prevNode
		prevNode.next = nil
	} else {
		prevNode.next = nextNode
		nextNode.prev = prevNode
	}

}
