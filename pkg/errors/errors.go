package errors

import (
	"github.com/giantswarm/microerror"
)

var InvalidConfigError = &microerror.Error{
	Kind: "invalidConfigError",
}

// IsInvalidConfig asserts invalidConfigError.
func IsInvalidConfig(err error) bool {
	return microerror.Cause(err) == InvalidConfigError
}

var FatalError = &microerror.Error{
	Kind: "FatalError",
}

// IsFatal asserts FatalError.
func IsFatal(err error) bool {
	return microerror.Cause(err) == FatalError
}
