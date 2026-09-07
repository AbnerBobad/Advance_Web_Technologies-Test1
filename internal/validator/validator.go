// Package validator provides a tiny, dependency-free validation helper that
// accumulates field errors and reports whether a value is valid.
package validator

// Validator collects validation error messages keyed by field name.
type Validator struct {
	Errors map[string]string
}

// New returns an empty validator ready for checks.
func New() *Validator {
	return &Validator{Errors: make(map[string]string)}
}

// Valid reports whether no validation errors were recorded.
func (v *Validator) Valid() bool {
	return len(v.Errors) == 0
}

// AddError stores the message for key unless that key already has an error.
func (v *Validator) AddError(key, message string) {
	if _, exists := v.Errors[key]; !exists {
		v.Errors[key] = message
	}
}

// Check adds an error for key if the given condition is false.
func (v *Validator) Check(ok bool, key, message string) {
	if !ok {
		v.AddError(key, message)
	}
}
