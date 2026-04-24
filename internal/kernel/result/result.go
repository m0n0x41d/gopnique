package result

type Result[T any] struct {
	value T
	err   error
}

func Ok[T any](value T) Result[T] {
	return Result[T]{value: value}
}

func Err[T any](err error) Result[T] {
	return Result[T]{err: err}
}

func (result Result[T]) Value() (T, error) {
	return result.value, result.err
}
