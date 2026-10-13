package graphql_test

import (
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// runRaceProgram writes source to a temporary module-aware program, runs it
// under the race detector with `go run -race` and fails the test if the
// detector produces any output. Subprocessing keeps races detectable even when
// the test binary itself is not run with `-race`, matching the prevailing CI
// configuration at the time it was introduced.
func runRaceProgram(t *testing.T, source string) {
	t.Helper()

	tempdir, err := ioutil.TempDir("", "race")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempdir)

	filename := filepath.Join(tempdir, "example.go")
	err = ioutil.WriteFile(filename, []byte(source), 0755)
	if err != nil {
		t.Fatal(err)
	}

	result, err := exec.Command("go", "run", "-race", filename).CombinedOutput()
	if err != nil || len(result) != 0 {
		t.Log(string(result))
		t.Fatal(err)
	}
}

// TestRace hammers two concurrent NewSchema calls against shared, lazily
// initialised type instances.
func TestRace(t *testing.T) {
	runRaceProgram(t, `
		package main

		import (
			"runtime"
			"sync"

			"github.com/sourcenetwork/graphql-go"
		)

		func main() {
			var wg sync.WaitGroup
			wg.Add(2)
			for i := 0; i < 2; i++ {
				go func() {
					defer wg.Done()
					schema, _ := graphql.NewSchema(graphql.SchemaConfig{
						Query: graphql.NewObject(graphql.ObjectConfig{
							Name: "RootQuery",
							Fields: graphql.Fields{
								"hello": &graphql.Field{
									Type: graphql.String,
									Resolve: func(p graphql.ResolveParams) (interface{}, error) {
										return "world", nil
									},
								},
							},
						}),
					})
					runtime.KeepAlive(schema)
				}()
			}

			wg.Wait()
		}
	`)
}

// TestRaceInputObjectFields covers the unsynchronised lazy initialisation of
// InputObject.Fields, which shared schemas make reachable from concurrent
// requests. Phase one hammers a standalone input object directly; phase two
// reproduces the reported validation stack by keeping the input object
// uninitialized at NewSchema time via a directive argument, since NewSchema
// only eagerly initialises input objects reachable through field arguments.
func TestRaceInputObjectFields(t *testing.T) {
	runRaceProgram(t, `
		package main

		import (
			"fmt"
			"os"
			"runtime"
			"sync"

			"github.com/sourcenetwork/graphql-go"
		)

		func main() {
			inputObject := graphql.NewInputObject(graphql.InputObjectConfig{
				Name: "Filter",
				Fields: (graphql.InputObjectConfigFieldMapThunk)(func() (graphql.InputObjectConfigFieldMap, error) {
					runtime.Gosched()
					return graphql.InputObjectConfigFieldMap{
						"value": &graphql.InputObjectFieldConfig{
							Type: graphql.String,
						},
					}, nil
				}),
			})

			// Phase 1: concurrent lazy initialisation of a standalone input object.
			var fieldsWG sync.WaitGroup
			fieldsWG.Add(2)
			for i := 0; i < 2; i++ {
				go func() {
					defer fieldsWG.Done()
					for j := 0; j < 100; j++ {
						_ = inputObject.Error()
						_ = inputObject.Fields()
						runtime.Gosched()
					}
				}()
			}
			fieldsWG.Wait()

			// Phase 2: the reported path. An input object referenced only by a
			// directive argument is never walked by the schema's typeMapReducer,
			// so it stays uninitialized in the built schema and validation
			// (ArgumentsOfCorrectTypeRule -> isValidLiteralValue -> Fields) has
			// to initialise it lazily while other requests validate too. Note a
			// separate instance from phase 1: config.Types would have eagerly
			// initialised it during NewSchema.
			directiveInputObject := graphql.NewInputObject(graphql.InputObjectConfig{
				Name: "DirectiveFilter",
				Fields: (graphql.InputObjectConfigFieldMapThunk)(func() (graphql.InputObjectConfigFieldMap, error) {
					runtime.Gosched()
					return graphql.InputObjectConfigFieldMap{
						"value": &graphql.InputObjectFieldConfig{
							Type: graphql.String,
						},
					}, nil
				}),
			})

			schema, err := graphql.NewSchema(graphql.SchemaConfig{
				Query: graphql.NewObject(graphql.ObjectConfig{
					Name: "RootQuery",
					Fields: graphql.Fields{
						"hello": &graphql.Field{
							Type: graphql.String,
							Resolve: func(p graphql.ResolveParams) (interface{}, error) {
								return "world", nil
							},
						},
					},
				}),
				Directives: []*graphql.Directive{
					graphql.NewDirective(graphql.DirectiveConfig{
						Name: "filterDirective",
						Locations: []string{
							graphql.DirectiveLocationQuery,
							graphql.DirectiveLocationField,
						},
						Args: graphql.FieldConfigArgument{
							"filter": &graphql.ArgumentConfig{
								Type: directiveInputObject,
							},
						},
					}),
				},
			})
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			var doWG sync.WaitGroup
			doWG.Add(2)
			for i := 0; i < 2; i++ {
				go func() {
					defer doWG.Done()
					for j := 0; j < 100; j++ {
						result := graphql.Do(graphql.Params{
							Schema:        schema,
							RequestString: "{ hello @filterDirective(filter: { value: \"x\" }) }",
						})
						if len(result.Errors) > 0 {
							fmt.Println(result.Errors[0])
							os.Exit(1)
						}
					}
				}()
			}
			doWG.Wait()
		}
	`)
}

// TestRaceInputObjectError runs NewSchema on two goroutines whose schemas share
// one input object with a failing fields thunk. A failed build leaves the input
// object uninitialised, so every NewSchema call rebuilds its fields and reads
// its error while the other goroutine may be rebuilding them.
func TestRaceInputObjectError(t *testing.T) {
	runRaceProgram(t, `
		package main

		import (
			"errors"
			"fmt"
			"os"
			"sync"

			"github.com/sourcenetwork/graphql-go"
		)

		func main() {
			errFields := errors.New("fields unavailable")
			filter := graphql.NewInputObject(graphql.InputObjectConfig{
				Name: "Filter",
				Fields: (graphql.InputObjectConfigFieldMapThunk)(func() (graphql.InputObjectConfigFieldMap, error) {
					return nil, errFields
				}),
			})

			var wg sync.WaitGroup
			wg.Add(2)
			for i := 0; i < 2; i++ {
				go func() {
					defer wg.Done()
					for j := 0; j < 200; j++ {
						_, err := graphql.NewSchema(graphql.SchemaConfig{
							Query: graphql.NewObject(graphql.ObjectConfig{
								Name: "RootQuery",
								Fields: graphql.Fields{
									"hello": &graphql.Field{
										Type: graphql.String,
										Args: graphql.FieldConfigArgument{
											"filter": &graphql.ArgumentConfig{Type: filter},
										},
									},
								},
							}),
						})
						if !errors.Is(err, errFields) {
							fmt.Println("NewSchema returned", err)
							os.Exit(1)
						}
					}
				}()
			}
			wg.Wait()
		}
	`)
}

// TestRaceEnumLookups runs concurrent requests on a shared schema that parse an
// enum argument and serialize an enum result, so both of the enum's lookups are
// read from several goroutines at once.
func TestRaceEnumLookups(t *testing.T) {
	runRaceProgram(t, `
		package main

		import (
			"fmt"
			"os"
			"sync"

			"github.com/sourcenetwork/graphql-go"
		)

		func main() {
			color := graphql.NewEnum(graphql.EnumConfig{
				Name: "Color",
				Values: graphql.EnumValueConfigMap{
					"RED":   &graphql.EnumValueConfig{Value: 0},
					"GREEN": &graphql.EnumValueConfig{Value: 1},
				},
			})
			schema, err := graphql.NewSchema(graphql.SchemaConfig{
				Query: graphql.NewObject(graphql.ObjectConfig{
					Name: "RootQuery",
					Fields: graphql.Fields{
						"color": &graphql.Field{
							Type: color,
							Args: graphql.FieldConfigArgument{
								"input": &graphql.ArgumentConfig{Type: color},
							},
							Resolve: func(p graphql.ResolveParams) (interface{}, error) {
								return p.Args["input"], nil
							},
						},
					},
				}),
			})
			if err != nil {
				fmt.Println(err)
				os.Exit(1)
			}

			var wg sync.WaitGroup
			wg.Add(2)
			for i := 0; i < 2; i++ {
				go func() {
					defer wg.Done()
					for j := 0; j < 100; j++ {
						result := graphql.Do(graphql.Params{
							Schema:        schema,
							RequestString: "{ color(input: GREEN) }",
						})
						if len(result.Errors) > 0 {
							fmt.Println(result.Errors[0])
							os.Exit(1)
						}
					}
				}()
			}
			wg.Wait()
		}
	`)
}
