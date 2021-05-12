package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden (.golden) files")

// This is the directory where our test fixtures are.
const fixtureDir = "./test-fixtures"

func TestRewrite(t *testing.T) {
	test := []struct {
		cfg  *config
		file string
	}{
		{
			file: "field_type_modify",
			cfg: &config{
				structName: "foo",
				fieldName:  "bar",
				from:       "string",
				to:         "[]byte",
			},
		},
	}

	for _, ts := range test {
		t.Run(ts.file, func(t *testing.T) {
			ts.cfg.file = filepath.Join(fixtureDir, fmt.Sprintf("%s.input", ts.file))

			node, err := ts.cfg.parse()
			if err != nil {
				t.Fatal(err)
			}

			start, end, err := ts.cfg.findSelection(node)
			if err != nil {
				t.Fatal(err)
			}

			rewrittenNode, err := ts.cfg.rewrite(node, start, end)
			if err != nil {
				t.Fatal(err)
			}

			out, err := ts.cfg.format(rewrittenNode)
			if err != nil {
				t.Fatal(err)
			}
			got := []byte(out)

			// update golden file if necessary
			golden := filepath.Join(fixtureDir, fmt.Sprintf("%s.golden", ts.file))
			if *update {
				err := ioutil.WriteFile(golden, got, 0644)
				if err != nil {
					t.Error(err)
				}
				return
			}

			// get golden file
			want, err := ioutil.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}

			from, err := ioutil.ReadFile(ts.cfg.file)
			if err != nil {
				t.Fatal(err)
			}

			// compare
			if !bytes.Equal(got, want) {
				t.Errorf("case %s\ngot:\n====\n\n%s\nwant:\n=====\n\n%s\nfrom:\n=====\n\n%s\n",
					ts.file, got, want, from)
			}
		})
	}
}

func TestParseConfig(t *testing.T) {
	// don't output help message during the test
	flag.CommandLine.SetOutput(ioutil.Discard)

	// The flag.CommandLine.Parse() call fails if there are flags re-defined
	// with the same name. If there are duplicates, parseConfig() will return
	// an error.
	_, err := parseConfig([]string{"test"})
	if err != nil {
		t.Fatal(err)
	}
}
