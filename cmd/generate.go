// Copyright © 2017 sleepinggenius2 <sleepinggenius2@users.noreply.github.com>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"
	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/models"
	"github.com/sleepinggenius2/gosmi/types"
	"github.com/spf13/cobra"
)

const fileHeader = `// Code generated by mib2go. DO NOT EDIT.
package %s

import (
	"github.com/sleepinggenius2/gosmi/models"
	"github.com/sleepinggenius2/gosmi/types"
)

`
const allowedNodeKinds = types.NodeScalar | types.NodeTable | types.NodeRow | types.NodeColumn | types.NodeNotification

var (
	outDir      string
	outFilename string
	packageName string
	paths       []string
)

func expandPath(path string) string {
	parts := strings.SplitN(path, string(filepath.Separator), 2)
	firstPart := parts[0]
	if firstPart == "" || firstPart[0] != '~' {
		return path
	}
	username := firstPart[1:]
	var (
		u   *user.User
		err error
	)
	if username == "" {
		u, err = user.Current()
	} else {
		u, err = user.Lookup(username)
	}
	if err != nil {
		return path
	}
	if len(parts) == 1 {
		return u.HomeDir
	}
	return filepath.Join(u.HomeDir, parts[1])
}

// generateCmd represents the generate command
var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generates Go files from MIBs",
	Long:  `Generates Go files from MIBs.`,
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		gosmi.Init()
		defer gosmi.Exit()

		for _, path := range paths {
			if path == "" {
				continue
			}
			switch path[0] {
			case '+':
				expandedPath := expandPath(path[1:])
				log.Println("Appending path", expandedPath)
				gosmi.AppendPath(expandedPath)
			case '-':
				expandedPath := expandPath(path[1:])
				log.Println("Prepending path", expandedPath)
				gosmi.PrependPath(expandedPath)
			default:
				expandedPath := expandPath(path)
				log.Println("Setting path", expandedPath)
				gosmi.SetPath(expandedPath)
			}
		}

		var out *os.File
		if outFilename == "-" {
			out = os.Stdout
		} else if outFilename != "" {
			var err error
			out, err = os.OpenFile(outFilename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				return errors.Wrapf(err, "Opening file %s", outFilename)
			}
			defer out.Close()
			log.Printf("Outputting to %s\n", outFilename)
		}

		typesMap := make(map[string]*models.Type)

		for _, arg := range args {
			_, err := gosmi.LoadModule(arg)
			if err != nil {
				return errors.Wrapf(err, "Loading module %s", arg)
			}
		}

		firstModule := true

		modules := gosmi.GetLoadedModules()
		for _, module := range modules {
			fileBuf := &bytes.Buffer{}

			generateMibFile(module, fileBuf, typesMap)

			if fileBuf.Len() == 0 {
				log.Printf("Module %s: Skipping empty module\n", module.Name)
				continue
			}

			outFile := out
			if outFile == nil {
				filename := filepath.Join(outDir, strings.ToLower(module.Name)+".go")
				outFile, err = os.OpenFile(filename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
				if err != nil {
					return errors.Wrapf(err, "Opening file %s", filename)
				}
				defer outFile.Close()
				log.Printf("Module %s: Outputting to %s\n", module.Name, filename)
			}

			writeHeader := out == nil || firstModule
			firstModule = false
			err = writeGoFile(outFile, fileBuf.Bytes(), writeHeader)
			if err != nil {
				return errors.Wrap(err, "Writing module Go file")
			}
		}

		typeKeys := make([]string, len(typesMap))
		var typeIndex int
		for key := range typesMap {
			typeKeys[typeIndex] = key
			typeIndex++
		}
		sort.Strings(typeKeys)

		typesBuf := &bytes.Buffer{}

		for _, key := range typeKeys {
			generateTypeBlock(typesBuf, typesMap[key], true)
		}

		outFile := out
		if outFile == nil {
			filename := filepath.Join(outDir, "types.go")
			outFile, err = os.OpenFile(filename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				return errors.Wrapf(err, "Opening file %s", filename)
			}
			defer outFile.Close()
			log.Printf("Types: Outputting to %s\n", filename)
		}

		writeHeader := out == nil
		err = writeGoFile(outFile, typesBuf.Bytes(), writeHeader)
		if err != nil {
			return errors.Wrap(err, "Writing types Go file")
		}

		return nil
	},
}

func formatModuleName(moduleName string) (formattedName string) {
	parts := strings.Split(moduleName, "-")
	for _, part := range parts {
		formattedName += strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return
}

func formatModuleVarName(moduleName string) (formattedName string) {
	formattedModuleName := formatModuleName(moduleName)
	return strings.ToLower(formattedModuleName[:1]) + formattedModuleName[1:] + "Module"
}

func formatNodeName(nodeName string) (formattedName string) {
	return strings.ToUpper(nodeName[:1]) + nodeName[1:]
}

func formatNodeVarName(nodeName string) (formattedName string) {
	return strings.ToLower(nodeName[:1]) + nodeName[1:] + "Node"
}

func generateMibFile(module gosmi.SmiModule, buf io.Writer, typesMap map[string]*models.Type) {
	nodes := module.GetNodes(allowedNodeKinds)
	if len(nodes) == 0 {
		return
	}

	generateModuleStruct(buf, module.Name, nodes)
	generateModuleVar(buf, module.Name, nodes)

	for _, node := range nodes {
		generateNodeVar(buf, node, typesMap)
	}
}

func generateModuleStruct(buf io.Writer, moduleName string, nodes []gosmi.SmiNode) {
	formattedModuleVarName := formatModuleVarName(moduleName)

	fmt.Fprintf(buf, "type %s struct {\n", formattedModuleVarName)
	for _, node := range nodes {
		fmt.Fprintf(buf, "\t%s\tmodels.%sNode\n", formatNodeName(node.Name), node.Kind)
	}
	fmt.Fprintf(buf, "}\n\n")
}

func generateModuleVar(buf io.Writer, moduleName string, nodes []gosmi.SmiNode) {
	formattedModuleName := formatModuleName(moduleName)
	formattedModuleVarName := formatModuleVarName(moduleName)

	fmt.Fprintf(buf, "var %s = %s {\n", formattedModuleName, formattedModuleVarName)
	for _, node := range nodes {
		fmt.Fprintf(buf, "\t%s:\t%s,\n", formatNodeName(node.Name), formatNodeVarName(node.Name))
	}
	fmt.Fprintf(buf, "}\n\n")
}

func generateNodeVar(buf io.Writer, node gosmi.SmiNode, typesMap map[string]*models.Type) {
	fmt.Fprintf(buf, "var %s = models.%sNode{\n", formatNodeVarName(node.Name), node.Kind)

	generateNodePartBaseNode(buf, node)

	switch node.Kind {
	case types.NodeColumn, types.NodeScalar:
		generateNodePartScalar(buf, node, typesMap)
	case types.NodeTable:
		generateNodePartTable(buf, node)
	case types.NodeRow:
		generateNodePartRow(buf, node)
	case types.NodeNotification:
		generateNodePartNotification(buf, node)
	}

	fmt.Fprintf(buf, "}\n")
}

func generateNodePartBaseNode(buf io.Writer, node gosmi.SmiNode) {
	oid := node.Oid
	oidFormatted := node.RenderNumeric()
	oidLen := node.OidLen
	if node.Kind == types.NodeScalar {
		oid = append(oid, 0)
		oidFormatted += ".0"
		oidLen++
	}

	fmt.Fprintf(buf, "\tBaseNode: models.BaseNode{\n")
	fmt.Fprintf(buf, "\t\tName: %q,\n", node.Name)
	fmt.Fprintf(buf, "\t\tOid: %#v,\n", oid)
	fmt.Fprintf(buf, "\t\tOidFormatted: %q,\n", oidFormatted)
	fmt.Fprintf(buf, "\t\tOidLen: %d,\n", oidLen)
	fmt.Fprintf(buf, "\t},\n")
}

func generateNodePartScalar(buf io.Writer, node gosmi.SmiNode, typesMap map[string]*models.Type) {
	switch node.Type.Name {
	case "Integer32", "OctetString", "ObjectIdentifier", "Unsigned32", "Integer64", "Unsigned64", "Enumeration", "Bits":
		generateTypeBlock(buf, node.Type, false)
	default:
		if _, ok := typesMap[node.Type.Name]; !ok {
			typesMap[node.Type.Name] = node.Type
		}
		fmt.Fprintf(buf, "\tType: %sType,\n", formatNodeName(node.Type.Name))
	}
}

func generateNodePartTable(buf io.Writer, node gosmi.SmiNode) {
	fmt.Fprintf(buf, "\tRow: %s,\n", formatNodeVarName(node.GetRow().Name))
}

func generateNodePartRow(buf io.Writer, node gosmi.SmiNode) {
	fmt.Fprintf(buf, "\tColumns: []models.ColumnNode{\n")
	_, columnOrder := node.GetColumns()
	for _, column := range columnOrder {
		fmt.Fprintf(buf, "\t\t%s,\n", formatNodeVarName(column))
	}
	fmt.Fprintf(buf, "\t},\n")
	fmt.Fprintf(buf, "\tIndex: []models.ColumnNode{\n")
	indices := node.GetIndex()
	for _, index := range indices {
		fmt.Fprintf(buf, "\t\t%s,\n", formatNodeVarName(index.Name))
	}
	fmt.Fprintf(buf, "\t},\n")
}

func generateNodePartNotification(buf io.Writer, node gosmi.SmiNode) {
	objects := node.GetNotificationObjects()
	fmt.Fprintf(buf, "\tObjects: []models.ScalarNode{\n")
	for _, object := range objects {
		if object.Kind == types.NodeScalar {
			fmt.Fprintf(buf, "\t\t%s,\n", formatNodeVarName(object.Name))
		} else {
			fmt.Fprintf(buf, "\t\tmodels.ScalarNode(%s),\n", formatNodeVarName(object.Name))
		}
	}
	fmt.Fprintf(buf, "\t},\n")
}

func generateTypeBlock(buf io.Writer, t *models.Type, asVar bool) {
	if asVar {
		fmt.Fprintf(buf, "var %sType = models.Type{\n", formatNodeName(t.Name))
	} else {
		fmt.Fprintf(buf, "Type: models.Type{\n")
	}
	fmt.Fprintf(buf, "\tBaseType: types.BaseType%s,\n", t.BaseType)
	if t.Enum != nil {
		fmt.Fprintf(buf, "\tEnum: &models.Enum{\n")
		fmt.Fprintf(buf, "\t\tBaseType: types.BaseType%s,\n", t.Enum.BaseType)
		fmt.Fprintf(buf, "\t\tValues: []models.NamedNumber{\n")
		for _, value := range t.Enum.Values {
			fmt.Fprintf(buf, "\t\t\t%#v,\n", value)
		}
		fmt.Fprintf(buf, "\t\t},\n")
		fmt.Fprintf(buf, "\t},\n")
	}
	if t.Format != "" {
		fmt.Fprintf(buf, "\tFormat: %q,\n", t.Format)
	}
	fmt.Fprintf(buf, "\tName: %q,\n", t.Name)
	if len(t.Ranges) > 0 {
		fmt.Fprintf(buf, "\tRanges: []models.Range{\n")
		for _, typeRange := range t.Ranges {
			fmt.Fprintf(buf, "\t\tmodels.Range{BaseType: types.BaseType%s, MinValue: %#v, MaxValue: %#v},\n",
				typeRange.BaseType,
				typeRange.MinValue,
				typeRange.MaxValue,
			)
		}
		fmt.Fprintf(buf, "\t},\n")
	}
	if t.Units != "" {
		fmt.Fprintf(buf, "\tUnits: %q,\n", t.Units)
	}
	if asVar {
		fmt.Fprintf(buf, "}\n\n")
	} else {
		fmt.Fprintf(buf, "},\n")
	}
}

func writeGoFile(out io.Writer, b []byte, writeHeader bool) error {
	formattedSource, err := format.Source(b)
	if err != nil {
		return errors.Wrap(err, "Generating formatted source")
	}

	if writeHeader {
		fmt.Fprintf(out, fileHeader, packageName)
	}

	_, err = out.Write(formattedSource)
	if err != nil {
		return errors.Wrap(err, "Writing file")
	}

	return nil
}

func init() {
	RootCmd.AddCommand(generateCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// generateCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	flags := generateCmd.Flags()
	flags.StringVarP(&outDir, "dir", "d", ".", "Output directory")
	flags.StringVarP(&outFilename, "output", "o", "", "Output filename, use - for stdout")
	flags.StringVarP(&packageName, "package", "p", "mibs", "The package for the generated file")
	flags.StringSliceVarP(&paths, "path", "M", []string{}, "Path(s) to add to MIB search path")
}
