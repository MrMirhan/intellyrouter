package batch

import "fmt"

// ItemError reports which item made a batch fail.
type ItemError struct {
	Index int
	Err   error
}

func (e *ItemError) Error() string {
	return fmt.Sprintf("item %d: %v", e.Index, e.Err)
}
