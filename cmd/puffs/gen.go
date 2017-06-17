// Use of this source code is governed by a BSD-style license that can be found
// in the LICENSE file.

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"github.com/google/puffs/lang/parse"
	"github.com/google/puffs/lang/render"
	"github.com/google/puffs/lang/token"
)

func doGen(puffsRoot string, args []string) error {
	flags := flag.NewFlagSet("gen", flag.ExitOnError)
	langsFlag := flags.String("langs", langsDefault, langsUsage)
	if err := flags.Parse(args); err != nil {
		return err
	}
	langs, err := parseLangs(*langsFlag)
	if err != nil {
		return err
	}
	args = flags.Args()
	if len(args) == 0 {
		args = []string{"std/..."}
	}

	for _, arg := range args {
		recursive := strings.HasSuffix(arg, "/...")
		if recursive {
			arg = arg[:len(arg)-4]
		}
		if err := gen(puffsRoot, arg, langs, recursive); err != nil {
			return err
		}
	}
	return nil
}

func gen(puffsRoot, dirname string, langs []string, recursive bool) error {
	filenames, dirnames, err := listDir(puffsRoot, dirname, recursive)
	if err != nil {
		return err
	}
	if len(filenames) > 0 {
		if err := genDir(puffsRoot, dirname, filenames, langs); err != nil {
			return err
		}
	}
	if len(dirnames) > 0 {
		for _, d := range dirnames {
			if err := gen(puffsRoot, dirname+"/"+d, langs, recursive); err != nil {
				return err
			}
		}
	}
	return nil
}

func genDir(puffsRoot string, dirname string, filenames []string, langs []string) error {
	// TODO: skip the generation if the output file already exists and its
	// mtime is newer than all inputs and the puffs-gen-foo command.

	tm := &token.Map{}
	buf := &bytes.Buffer{}
	for _, filename := range filenames {
		filename = filepath.Join(puffsRoot, filepath.FromSlash(dirname), filename)
		src, err := ioutil.ReadFile(filename)
		if err != nil {
			return err
		}
		tokens, _, err := token.Tokenize(tm, filename, src)
		if err != nil {
			return err
		}
		// We don't need the AST node to pretty-print, but it's worth rejecting
		// syntax errors early. This is just a parse, not a full type check.
		if _, err := parse.Parse(tm, filename, tokens); err != nil {
			return err
		}
		if err := render.Render(buf, tm, tokens, nil); err != nil {
			return err
		}
	}
	combinedSrc := buf.Bytes()

	packageName := path.Base(dirname)
	if !validName(packageName) {
		return fmt.Errorf(`invalid package %q, not in [a-z0-9]+`, packageName)
	}

	for _, lang := range langs {
		command := "puffs-gen-" + lang
		stdout := &bytes.Buffer{}
		cmd := exec.Command(command, "-package_name", packageName)
		cmd.Stdin = bytes.NewReader(combinedSrc)
		cmd.Stdout = stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err == nil {
			// No-op.
		} else if _, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%s: failed", command)
		} else {
			return err
		}
		out := stdout.Bytes()
		if err := genFile(puffsRoot, dirname, lang, out); err != nil {
			return err
		}

		// Special-case the "c" generator to also write a .h file.
		if lang != "c" {
			continue
		}
		if i := bytes.Index(out, cHeaderEndsHere); i < 0 {
			return fmt.Errorf("%s: output did not contain %q", command, cHeaderEndsHere)
		} else {
			out = out[:i]
		}
		if err := genFile(puffsRoot, dirname, "h", out); err != nil {
			return err
		}
	}
	return nil
}

var cHeaderEndsHere = []byte("\n// C HEADER ENDS HERE.\n\n")

func genFile(puffsRoot string, dirname string, lang string, out []byte) error {
	outFilename := filepath.Join(puffsRoot, "gen", lang, filepath.FromSlash(dirname)+"."+lang)
	if existing, err := ioutil.ReadFile(outFilename); err == nil && bytes.Equal(existing, out) {
		fmt.Println("gen unchanged: ", outFilename)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outFilename), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(outFilename, out, 0644); err != nil {
		return err
	}
	fmt.Println("gen wrote:     ", outFilename)
	return nil
}
