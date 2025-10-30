package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/template"
)

const (
	zh_CN = "zh_CN"
	en_US = "en_US"
)

var localeMap map[string]map[string]interface{}

func main() {
	if len(os.Args) != 3 {
		printUsage()
		exitWithError("Error: Need exactly 2 arguments")
	}

	if err := readLocalesFromFile(os.Args[1]); err != nil {
		exitWithError(fmt.Sprintf("readLocalesFromFile: %v", err))
	}

	if err := parseTemplate(os.Args[2]); err != nil {
		exitWithError(fmt.Sprintf("parseTemplate: %v", err))
	}
}

func printUsage() {
	fmt.Printf("usage: %s [locale-file] [locale]\n", os.Args[0])
}

func exitWithError(message string) {
	fmt.Fprintln(os.Stderr, message)
	os.Exit(1)
}

func readLocalesFromFile(file string) error {
	b, err := os.ReadFile(file)
	if err != nil {
		return err
	}

	return json.Unmarshal(b, &localeMap)
}

func parseTemplate(lang string) error {
	tmpl, err := template.ParseFiles("template.sh")
	if err != nil {
		return err
	}

	switch lang {
	case zh_CN:
		file, err := os.Create("install.sh")
		if err != nil {
			return err
		}
		defer file.Close()
		return tmpl.Execute(file, localeMap[zh_CN])
	case en_US:
		file, err := os.Create("install_en.sh")
		if err != nil {
			return err
		}
		defer file.Close()
		return tmpl.Execute(file, localeMap[en_US])
	default:
		return fmt.Errorf("unsupported locale: %s", lang)
	}
}
