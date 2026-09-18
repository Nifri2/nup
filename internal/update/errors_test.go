package update

import "errors"

// errorsAs is a tiny alias so the tests read a little more directly.
func errorsAs(err error, target any) bool { return errors.As(err, target) }
