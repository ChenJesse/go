package snapshotter

import (
	"encoding/json"
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/kylelemons/godebug/pretty"
	"github.com/pmezard/go-difflib/difflib"
)

type T interface {
	Name() string
	Errorf(format string, args ...interface{})
	Helper()
}

var rewrite = flag.Bool("rewriteSnapshots", false, "rewrite test data output")

func jsonRoundTrip(value interface{}) (interface{}, error) {
	bytes, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var roundtripped interface{}
	if err := json.Unmarshal(bytes, &roundtripped); err != nil {
		return nil, err
	}
	return roundtripped, nil
}

type snapshot struct {
	Name   string
	Values []interface{}
}

// Snapshotter is a utility for writing snapshot tests. In a snapshot, the
// expected test output is generated by running the test and storing the
// output, which for complex test outputs would otherwise require a lot of
// effort to write.
type Snapshotter struct {
	t              T
	name           string
	snapshots      []*snapshot
	SnapshotErrors bool
}

// New creates a new Snapshotter. Any errors encountered will fail
// the test.
func New(t T) *Snapshotter {
	return &Snapshotter{t: t}
}

func NewNamed(t T, name string) *Snapshotter {
	return &Snapshotter{t: t, name: name}
}

// Snapshot records a value for a snapshot test. For the test to pass, all
// invocations to Snapshot should have the same arguments. All values should be
// JSON-marshalable.
func (s *Snapshotter) Snapshot(name string, values ...interface{}) {
	for i, value := range values {
		roundtripped, err := jsonRoundTrip(value)
		if err != nil {
			s.t.Errorf("%s: error roundtripping value %v: %s", name, value, err)
			return
		}
		values[i] = roundtripped
	}

	s.snapshots = append(s.snapshots, &snapshot{
		Name:   name,
		Values: values,
	})
}

// Verify finishes a snapshot test. It either compares the test output, or it
// rewrites the test output.
func (s *Snapshotter) Verify() {
	s.t.Helper()
	nameSuffix := ""
	if s.name != "" {
		nameSuffix = "_" + strings.Replace(strings.Replace(s.name, "/", "-", -1), ":", "-", -1)
	}
	name := filepath.Join("testdata", strings.Replace(strings.Replace(s.t.Name(), "/", "-", -1), ":", "-", -1)+nameSuffix+".snapshots.json")
	if *rewrite {
		// If there are no snapshots, then when rewriting, we want to remove the file if it exists.
		if len(s.snapshots) == 0 {
			if _, err := os.Stat(name); os.IsNotExist(err) {
				return
			}

			// The file exists, so let's remove it.
			err := os.Remove(name)
			if err != nil {
				s.t.Errorf("failed to remove the existing snapshot file %s", name)
			}
			return
		}
		if err := os.MkdirAll("testdata", 0755); err != nil {
			s.t.Errorf("error creating testdata directory: %s", err)
			return
		}
		bytes, err := json.MarshalIndent(s.snapshots, "", "  ")
		if err != nil {
			s.t.Errorf("error marshaling snapshots: %s", err)
			return
		}
		if err := ioutil.WriteFile(name, bytes, 0644); err != nil {
			s.t.Errorf("error writing snapshots: %s", err)
			return
		}
	} else {
		// When no snapshots file exists and no snapshots have been taken, do nothing.
		if _, err := os.Stat(name); os.IsNotExist(err) && len(s.snapshots) == 0 {
			return
		}

		bytes, err := ioutil.ReadFile(name)
		if err != nil {
			s.t.Errorf("error reading snapshots: %s", err)
			return
		}
		var expected []*snapshot
		if err := json.Unmarshal(bytes, &expected); err != nil {
			s.t.Errorf("error unmarshaling snapshots: %s", err)
			return
		}

		actual := s.snapshots

		var actualNames, expectedNames []string
		for _, snapshot := range actual {
			actualNames = append(actualNames, snapshot.Name)
		}
		for _, snapshot := range expected {
			expectedNames = append(expectedNames, snapshot.Name)
		}

		if diff := diffString(expectedNames, actualNames); diff != "" {
			s.t.Errorf("snapshot names differ:\n%s", diff)
			return
		}

		for i := range actual {
			expectedValue := expected[i].Values
			actualValue := actual[i].Values

			if len(expectedValue) == 1 && len(actualValue) == 1 {
				if expectedString, ok := expectedValue[0].(string); ok {
					if actualString, ok := actualValue[0].(string); ok {
						if diff := diffString(expectedString, actualString); diff != "" {
							s.t.Errorf("snapshot %s differs:\n%s", actual[i].Name, diff)
							s.t.Errorf("If this is intentional, you can run `go test . -rewriteSnapshots` to generate new snapshots.")
							return
						}
					}
				}
			}

			if diff := diffString(expected[i].Values, actual[i].Values); diff != "" {
				s.t.Errorf("snapshot %s differs:\n%s", actual[i].Name, diff)
				s.t.Errorf("If this is intentional, you can run `go test . -rewriteSnapshots` to generate new snapshots.")
			}
		}
	}
}

func coerceToString(i interface{}) string {
	if str, ok := i.(string); ok {
		return str
	}
	return pretty.Sprint(i)
}

func diffString(a, b interface{}) string {
	str, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(coerceToString(a)),
		B:        difflib.SplitLines(coerceToString(b)),
		FromFile: "expected",
		ToFile:   "received",
		Context:  1,
	})

	return str
}
