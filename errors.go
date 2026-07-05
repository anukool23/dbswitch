package dbswitch

// DuplicateError reports a unique-constraint violation. It carries the name of
// the violated constraint so the caller can map it to a domain meaning — e.g.
// "users_email_key" -> "email already taken". The library is generic and
// cannot itself know what a given constraint *means*.
type DuplicateError struct {
	Constraint string
}

func (e *DuplicateError) Error() string {
	return "dbswitch: unique constraint violation: " + e.Constraint
}

// Is makes errors.Is(err, ErrDuplicate) return true for any DuplicateError,
// while errors.As(err, &target) still recovers the constraint name.
func (e *DuplicateError) Is(target error) bool {
	return target == ErrDuplicate
}
