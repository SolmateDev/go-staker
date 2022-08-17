package ring

import "errors"

type node[T any] struct {
	value T
	id    uint
}

type Ring[T any] struct {
	list   []*node[T]
	start  uint
	length uint
	lastId uint
}

func Create[T any](size uint64) (*Ring[T], error) {
	if size == 0 {
		return nil, errors.New("size is zero")
	}
	r := new(Ring[T])
	r.list = make([]*node[T], size)
	r.start = 0
	r.length = 0
	r.lastId = 0

	return r, nil
}

// returns the size of the buffer
func (r *Ring[T]) Max() uint {
	return uint(len(r.list))
}

// returns the number of elements in the buffer
func (r *Ring[T]) Length() uint {
	return r.length
}

// add an element to the end of the buffer
func (r *Ring[T]) Append(value T) (uint, error) {
	if r.Length() == r.Max()-1 {
		return 0, errors.New("buffer is full")
	}
	i := (r.start + r.length) % r.Max()
	id := r.lastId
	r.list[i] = &node[T]{value: value, id: id}
	r.length++
	r.lastId++
	return id, nil
}

// remove and return the first element from the buffer
func (r *Ring[T]) Pop() (value T, err error) {
	if r.Length() == 0 {
		err = errors.New("buffer is empty")
		return
	} else {
		n := r.list[r.start]
		r.start = (r.start + 1) % r.Max()
		r.length = r.length - 1
		value = n.value
		return
	}
}

// retrieve the Ith element from the buffer
func (r *Ring[T]) Get(i uint) (value T, err error) {
	if i < r.Length() {
		k := (r.start + i) % r.Max()
		value = r.list[k].value
	} else {
		err = errors.New("requested element does not lie in buffer")
	}
	return
}

// retrieve element by id
func (r *Ring[T]) GetById(id uint) (value T, err error) {
	i := id % r.Max()
	if i < r.Length() {
		k := (r.start + i) % r.Max()
		if r.list[k].id == id {
			value = r.list[k].value
		} else {
			err = errors.New("id not present")
		}
	} else {
		err = errors.New("requested element does not lie in buffer")
	}

	return
}
