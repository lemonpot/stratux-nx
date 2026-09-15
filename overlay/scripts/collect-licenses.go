package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._+-]+`)

func safeName(value string) string {
	return strings.Trim(unsafeName.ReplaceAllString(value, "_"), "_")
}

func copyFile(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	out, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyLicenseFiles(source, destination string) ([]string, error) {
	var files []string
	for _, pattern := range []string{"LICENSE*", "LICENCE*", "COPYING*", "NOTICE*"} {
		matches, err := filepath.Glob(filepath.Join(source, pattern))
		if err != nil {
			return nil, err
		}
		files = append(files, matches...)
	}
	sort.Strings(files)
	var copied []string
	for _, sourceFile := range files {
		info, err := os.Stat(sourceFile)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		target := filepath.Join(destination, filepath.Base(sourceFile))
		if err := copyFile(sourceFile, target); err != nil {
			return nil, err
		}
		copied = append(copied, target)
	}
	return copied, nil
}

func main() {
	sourceFlag := flag.String("source", "", "Stratux source directory")
	outputFlag := flag.String("output", "", "License output directory")
	flag.Parse()
	if *sourceFlag == "" || *outputFlag == "" {
		fmt.Fprintln(os.Stderr, "source and output are required")
		os.Exit(2)
	}
	source, _ := filepath.Abs(*sourceFlag)
	output, _ := filepath.Abs(*outputFlag)
	if err := os.RemoveAll(output); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(output, 0755); err != nil {
		panic(err)
	}

	index := []string{"Stratux NX bundled license files", ""}
	direct := map[string]string{
		"stratux-upstream":   filepath.Join(source, "LICENSE"),
		"stratux-nx":         filepath.Join(source, "debian", "stratux-nx-license"),
		"stratux-nx-notices": filepath.Join(source, "debian", "STRATUX-NX-NOTICES.md"),
	}
	directNames := []string{"stratux-upstream", "stratux-nx", "stratux-nx-notices"}
	for _, name := range directNames {
		path := direct[name]
		if _, err := os.Stat(path); err != nil {
			panic(fmt.Errorf("required license file is missing: %s", path))
		}
		target := filepath.Join(output, name+"-"+filepath.Base(path))
		if err := copyFile(path, target); err != nil {
			panic(err)
		}
		index = append(index, name+": "+filepath.Base(target))
	}

	for _, relative := range []string{"dump978", "dump1090", "rtl-ais", "ogn/ogn-tracker"} {
		destination := filepath.Join(output, safeName(relative))
		copied, err := copyLicenseFiles(filepath.Join(source, relative), destination)
		if err != nil {
			panic(err)
		}
		if len(copied) > 0 {
			index = append(index, relative+": "+filepath.Base(destination)+"/")
		}
	}

	command := exec.Command("go", "list", "-m", "-json", "all")
	command.Dir = source
	result, err := command.Output()
	if err != nil {
		panic(err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(result)))
	moduleCount := 0
	for {
		var module struct {
			Path string
			Dir  string
			Main bool
		}
		if err := decoder.Decode(&module); err == io.EOF {
			break
		} else if err != nil {
			panic(err)
		}
		if module.Main || module.Path == "" || module.Dir == "" {
			continue
		}
		destination := filepath.Join(output, "go-modules", safeName(module.Path))
		copied, err := copyLicenseFiles(module.Dir, destination)
		if err != nil {
			panic(err)
		}
		if len(copied) > 0 {
			moduleCount++
			index = append(index, module.Path+": go-modules/"+filepath.Base(destination)+"/")
		}
	}
	if moduleCount == 0 {
		panic("no Go module license files were collected")
	}
	if err := os.WriteFile(filepath.Join(output, "INDEX.txt"), []byte(strings.Join(index, "\n")+"\n"), 0644); err != nil {
		panic(err)
	}
	fmt.Printf("Collected license files for %d Go modules.\n", moduleCount)
}
