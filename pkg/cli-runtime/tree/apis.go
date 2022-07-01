package tree

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime"
)

type ApiResource struct {
	ApiRsrc metav1.APIResource
	GV      schema.GroupVersion
}

func (a ApiResource) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    a.GV.Group,
		Version:  a.GV.Version,
		Resource: a.ApiRsrc.Name,
	}
}

type resourceNameLookup map[string][]ApiResource

type resourceMap struct {
	list []ApiResource
	m    resourceNameLookup
}

func (rm *resourceMap) Lookup(s string) []ApiResource {
	return rm.m[strings.ToLower(s)]
}

func (rm *resourceMap) Resources() []ApiResource { return rm.list }

func FullAPIName(a ApiResource) string {
	sgv := a.GroupVersionResource()
	return strings.Join([]string{sgv.Resource, sgv.Version, sgv.Group}, ".")
}

func FindAPIs(ctx context.Context, c *cli.Config) (*resourceMap, error) {
	// workload := &cartov1alpha1.Workload{}
	// start := time.Now()
	// dc, err := cf.ToDiscoveryClient()
	// if err != nil {
	// 	return err
	// }

	client := c.Client.Discovery()
	resList, err := client.ServerPreferredResources()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch api groups from kubernetes: %w", err)
	}
	// c.Infof("queried api discovery in %v", time.Since(start))
	// c.Infof("found %d items (groups) in server-preferred APIResourceList", len(resList))

	rm := &resourceMap{
		m: make(resourceNameLookup),
	}
	for _, group := range resList {
		// c.Infof("iterating over group %s/%s (%d apis)", group.GroupVersion, group.APIVersion, len(group.APIResources))
		gv, err := schema.ParseGroupVersion(group.GroupVersion)
		if err != nil {
			return nil, fmt.Errorf("%q cannot be parsed into groupversion: %w", group.GroupVersion, err)
		}

		for _, apiRes := range group.APIResources {
			// c.Infof("  api=%s namespaced=%v", apiRes.Name, apiRes.Namespaced)
			if !contains(apiRes.Verbs, "list") {
				// c.Infof("    api (%s) doesn't have required verb, skipping: %v", apiRes.Name, apiRes.Verbs)
				continue
			}
			v := ApiResource{
				GV:      gv,
				ApiRsrc: apiRes,
			}
			names := apiNames(apiRes, gv)
			// c.Infof("names: %s", strings.Join(names, ", "))
			for _, name := range names {
				rm.m[name] = append(rm.m[name], v)
			}
			rm.list = append(rm.list, v)
		}
	}
	// c.Infof("  found %d apis", len(rm.m))
	return rm, nil
}

func contains(v []string, s string) bool {
	for _, vv := range v {
		if vv == s {
			return true
		}
	}
	return false
}

// return all names that could refer to this APIResource
func apiNames(a metav1.APIResource, gv schema.GroupVersion) []string {
	var out []string
	singularName := a.SingularName
	if singularName == "" {
		// TODO(ahmetb): sometimes SingularName is empty (e.g. Deployment), use lowercase Kind as fallback - investigate why
		singularName = strings.ToLower(a.Kind)
	}
	pluralName := a.Name
	shortNames := a.ShortNames
	names := append([]string{singularName, pluralName}, shortNames...)
	for _, n := range names {
		fmtBare := n                                                                // e.g. deployment
		fmtWithGroup := strings.Join([]string{n, gv.Group}, ".")                    // e.g. deployment.apps
		fmtWithGroupVersion := strings.Join([]string{n, gv.Version, gv.Group}, ".") // e.g. deployment.v1.apps

		out = append(out,
			fmtBare, fmtWithGroup, fmtWithGroupVersion)
	}
	return out
}
