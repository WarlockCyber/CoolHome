package tuya

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError is a business-level error returned by the Tuya cloud
// (HTTP 200 with "success": false, or an HTTP error status).
type APIError struct {
	Code   int
	Msg    string
	Status int
	TID    string
}

func (e *APIError) Error() string {
	if e.TID != "" {
		return fmt.Sprintf("tuya api error %d: %s (tid %s)", e.Code, e.Msg, e.TID)
	}
	return fmt.Sprintf("tuya api error %d: %s", e.Code, e.Msg)
}

// Documented Tuya codes for an unusable access token.
const (
	codeTokenInvalid = 1010
	codeTokenExpired = 1011
)

// Documented Tuya codes for a resource the project may not touch. They almost
// always mean the Cloud project is not wired up (services not authorized, no
// linked app account, foreign device) rather than a bug in the request.
const (
	codePermissionDeny       = 1106
	codePermissionDenyLegacy = 1100
)

// codeNotSupportDevice is what the cloud answers for a device it cannot describe
// over the data-point API at all. RF switches paired to an IR blaster, and the
// blaster itself, report this: they carry no thing model, so the device is not
// broken and retrying will not change the answer.
const codeNotSupportDevice = 2009

// codeNoPath is what the cloud answers for an endpoint that does not exist for
// a device: asking a product for a thing model it has none of.
const codeNoPath = 1108

// codeNoThingModel marks a device whose product model document came back empty.
// It is CoolHome's own code, not the cloud's: it is negative and permanent, so
// callers cache it like 2009 rather than retrying.
const codeNoThingModel = 2009

// codeServiceNotSubscribed is what the cloud answers on an API surface the
// project has not subscribed. It is a deployment step, not a failing request.
const codeServiceNotSubscribed = 28841101

func isTokenError(err error) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	return api.Code == codeTokenInvalid || api.Code == codeTokenExpired || api.Status == 401
}

func isPermissionError(err error) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	return api.Code == codePermissionDeny || api.Code == codePermissionDenyLegacy || api.Status == http.StatusForbidden
}

// IsUnsupportedDevice reports a device the cloud exposes without a thing model.
// Callers use it to stop retrying and to explain the device instead of showing
// an API error.
func IsUnsupportedDevice(err error) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	return api.Code == codeNotSupportDevice
}

// isDeviceWithoutModel reports the cloud's answer that a device has no legacy
// model to read: no specification, and no path to one.
func isDeviceWithoutModel(err error) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	return api.Code == codeNotSupportDevice || api.Code == codeNoPath
}

// codeQuotaExhausted is the cloud's answer when the project's controllable
// device pool is used up: the request is well formed, and no device will accept
// a write until the plan in the Tuya console changes.
const codeQuotaExhausted = 60001001

// IsQuotaExhausted reports a write refused because the Cloud project's
// controllable-device quota is used up. It is a subscription condition, not a
// fault of the device or of the request.
func IsQuotaExhausted(err error) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	return api.Code == codeQuotaExhausted
}

// IsServiceNotSubscribed reports an API surface the Cloud project has not
// subscribed. The request is well-formed; the project is missing a step in the
// Tuya console, so retrying on the next poll cannot help.
func IsServiceNotSubscribed(err error) bool {
	var api *APIError
	if !errors.As(err, &api) {
		return false
	}
	return api.Code == codeServiceNotSubscribed
}

// permissionHint turns a bare "permission deny" into something a human can act
// on. The cloud reports the same code for several unrelated misconfigurations,
// so the hint names the checks that actually resolve it.
func permissionHint(err error) error {
	if !isPermissionError(err) {
		return err
	}
	return fmt.Errorf("%w: check that the Cloud project has an authorized Smart Home "+
		"Device Management service and a linked app account (Cloud → Devices → "+
		"Link Tuya App Account), and that TUYA_REGION matches the account's data center", err)
}
