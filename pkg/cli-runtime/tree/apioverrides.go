package tree

import "strings"

// overrideType hardcodes lookup overrides for certain service types
func OverrideType(kind string, v *resourceMap) (ApiResource, bool) {
	kind = strings.ToLower(kind)

	switch kind {
	case "svc", "service", "services": // Knative also registers "Service", prefer v1.Service
		out := v.Lookup("service.v1.")
		if len(out) != 0 {
			return out[0], true
		}

	case "deploy", "deployment", "deployments": // most clusters will have Deployment in apps/v1 and extensions/v1beta1, extensions/v1/beta2
		out := v.Lookup("deployment.v1.apps")
		if len(out) != 0 {
			return out[0], true
		}
		out = v.Lookup("deployment.v1beta1.extensions")
		if len(out) != 0 {
			return out[0], true
		}
	}
	return ApiResource{}, false
}
