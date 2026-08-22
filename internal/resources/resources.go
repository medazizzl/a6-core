package resources

import "fmt"

type BlockingResource struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Players int    `json:"players,omitempty"`
}

type Checker func() []BlockingResource

func NoBlocking() []BlockingResource { return nil }

type ErrBlocked struct {
	Blocking []BlockingResource
}

func (e *ErrBlocked) Error() string {
	return fmt.Sprintf("resources: %d blocking resource(s) prevent this action without force", len(e.Blocking))
}
