package dns

import "github.com/giantswarm/microerror"

// IsIngressNotRead asserts ingressNotReadyError.
func IsIngressNotReady(err error) bool {
	return microerror.Cause(err) == ingressNotReadyError
}

var ingressNotReadyError = &microerror.Error{
	Kind: "ingressNotReadyError",
}

// IsTooManyICServices asserts tooManyICServicesError.
func IsTooManyICServices(err error) bool {
	return microerror.Cause(err) == tooManyICServicesError
}

var tooManyICServicesError = &microerror.Error{
	Kind: "tooManyICServicesError",
}
