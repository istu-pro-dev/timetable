package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	apispec "github.com/istu-pro-dev/timetable/backend/api"
)

type openAPIDoc struct {
	OpenAPI string                    `yaml:"openapi"`
	Paths   map[string]map[string]any `yaml:"paths"`
}

var httpMethods = []string{"get", "put", "post", "delete", "patch", "head", "options"}

func specOperations(t *testing.T) []string {
	t.Helper()
	var doc openAPIDoc
	if err := yaml.Unmarshal(apispec.OpenAPI, &doc); err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	if !strings.HasPrefix(doc.OpenAPI, "3.1") {
		t.Fatalf("openapi = %q, want 3.1.x", doc.OpenAPI)
	}
	var ops []string
	for path, item := range doc.Paths {
		for method := range item {
			if slices.Contains(httpMethods, method) {
				ops = append(ops, strings.ToUpper(method)+" "+path)
			}
		}
	}
	slices.Sort(ops)
	return ops
}

// TestSpecCoversRoutes checks that every registered route is documented and the spec
// documents no route that does not exist.
func TestSpecCoversRoutes(t *testing.T) {
	ops := specOperations(t)
	routes := slices.Clone(newServer(Deps{}).routes)
	slices.Sort(routes)

	for _, r := range routes {
		if _, found := slices.BinarySearch(ops, r); !found {
			t.Errorf("route %q is not in api/openapi.yaml", r)
		}
	}
	for _, op := range ops {
		if _, found := slices.BinarySearch(routes, op); !found {
			t.Errorf("spec operation %q has no registered route", op)
		}
	}
}

// TestSpecRefsResolve checks that every local $ref points to an existing node.
func TestSpecRefsResolve(t *testing.T) {
	var root yaml.Node
	if err := yaml.Unmarshal(apispec.OpenAPI, &root); err != nil {
		t.Fatal(err)
	}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "$ref" {
					if err := resolveRef(&root, n.Content[i+1].Value); err != nil {
						t.Error(err)
					}
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&root)
}

func resolveRef(root *yaml.Node, ref string) error {
	path, ok := strings.CutPrefix(ref, "#/")
	if !ok {
		return fmt.Errorf("non-local $ref %q", ref)
	}
	n := root.Content[0]
	for _, key := range strings.Split(path, "/") {
		next := (*yaml.Node)(nil)
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == key {
					next = n.Content[i+1]
					break
				}
			}
		}
		if next == nil {
			return fmt.Errorf("unresolved $ref %q", ref)
		}
		n = next
	}
	return nil
}

func TestServeOpenAPI(t *testing.T) {
	rec := httptest.NewRecorder()
	NewRouter(Deps{}).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/openapi.yaml", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Content-Type"), "yaml") ||
		!strings.HasPrefix(rec.Body.String(), "openapi: 3.1") {
		t.Fatalf("GET /api/openapi.yaml: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
