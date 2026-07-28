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

// TransactionFailedError is returned by TransactWrite when the transaction is
// rolled back. It wraps the underlying cause so the caller can inspect it with
// errors.Unwrap / errors.As — for example to check if the cause is a
// *DuplicateError from a conflicting Insert inside the transaction.
type TransactionFailedError struct {
	Cause error
}

func (e *TransactionFailedError) Error() string {
	return "dbswitch: transaction failed: " + e.Cause.Error()
}

func (e *TransactionFailedError) Unwrap() error { return e.Cause }

// Is makes errors.Is(err, ErrTransactionFailed) return true for any
// TransactionFailedError, while errors.As + Unwrap still reach the cause.
func (e *TransactionFailedError) Is(target error) bool {
	return target == ErrTransactionFailed
}
