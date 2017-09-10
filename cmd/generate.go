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
	"log"
	"os"
	"strings"

	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/types"
	"github.com/spf13/cobra"
)

var (
	outFilename string
	packageName string
	paths       []string
)

// generateCmd represents the generate command
var generateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generates a Go file from a MIB",
	Long:  `Generates a Go file from a MIB.`,
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		gosmi.Init()
		defer gosmi.Exit()

		for _, path := range paths {
			gosmi.AppendPath(path)
		}

		out := os.Stdout
		if outFilename != "" && outFilename != "-" {
			var err error
			out, err = os.OpenFile(outFilename, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644)
			if err != nil {
				log.Fatalf("Error opening file: %v\n", err)
			}
			defer out.Close()
			log.Printf("Outputting to %s\n", outFilename)
		}

		buf := &bytes.Buffer{}

		for _, arg := range args {
			moduleName, err := gosmi.LoadModule(arg)
			if err != nil {
				log.Fatalf("Error loading module [%s]: %v\n", arg, err)
			}

			module, err := gosmi.GetModule(moduleName)
			if err != nil {
				log.Fatalf("Error getting module [%s]: %v\n", moduleName, err)
			}

			formattedModuleName := formatModuleName(moduleName)

			fmt.Fprintf(buf, "package %s\n\n", packageName)
			fmt.Fprintln(buf,
				`import (
	"github.com/sleepinggenius2/gosmi"
	"github.com/sleepinggenius2/gosmi/models"
	"github.com/sleepinggenius2/gosmi/types"
)`)
			fmt.Fprintf(buf, "\ntype %sModule struct {\n", formattedModuleName)

			nodes := module.GetNodes()
			for _, node := range nodes {
				if node.Kind&(types.NodeScalar|types.NodeTable|types.NodeRow|types.NodeColumn|types.NodeNotification) > 0 {
					fmt.Fprintf(buf, "\t%s\tmodels.%sNode\n", formatNodeName(node.Name), node.Kind)
				}
			}
			fmt.Fprintln(buf, "}")

			fmt.Fprintf(buf, "\nvar %s = %sModule{", formattedModuleName, formattedModuleName)

			for _, node := range nodes {
				if node.Kind&(types.NodeScalar|types.NodeTable|types.NodeRow|types.NodeColumn|types.NodeNotification) > 0 {
					fmt.Fprintln(buf)

					fmt.Fprintf(buf, "\t%s: models.%sNode{\n", formatNodeName(node.Name), node.Kind)
					fmt.Fprintf(buf, "\t\tName: %q,\n", node.Name)
					oid := node.Oid
					oidFormatted := node.RenderNumeric()
					oidLen := node.OidLen
					if node.Kind == types.NodeScalar {
						oid = append(oid, 0)
						oidFormatted += ".0"
						oidLen++
					}
					fmt.Fprintf(buf, "\t\tOid: %#v,\n", oid)
					fmt.Fprintf(buf, "\t\tOidFormatted: %q,\n", oidFormatted)
					fmt.Fprintf(buf, "\t\tOidLen: %d,\n", oidLen)

					if node.Kind&(types.NodeColumn|types.NodeScalar) > 0 {
						fmt.Fprintln(buf, "\t\tType: gosmi.Type{")
						fmt.Fprintf(buf, "\t\t\tBaseType: types.BaseType%s,\n", node.Type.BaseType)
						if node.Type.Enum != nil {
							fmt.Fprintln(buf, "\t\t\tEnum: &gosmi.Enum{")
							fmt.Fprintf(buf, "\t\t\t\tBaseType: types.BaseType%s,\n\t\t\t\tValues: []gosmi.NamedNumber{\n", node.Type.Enum.BaseType)
							for _, value := range node.Type.Enum.Values {
								fmt.Fprintf(buf, "\t\t\t\t\t%#v,\n", value)
							}
							fmt.Fprintln(buf, "\t\t\t\t},\n\t\t\t},")
						}
						if node.Type.Format != "" {
							fmt.Fprintf(buf, "\t\t\tFormat: %q,\n", node.Type.Format)
						}
						// TODO: fmt.Fprintf(buf, "\t\tFormatter: %#v,\n", node.GetValueFormatter(gosmi.FormatAll))
						fmt.Fprintf(buf, "\t\t\tName: %q,\n", node.Type.Name)
						if len(node.Type.Ranges) > 0 {
							fmt.Fprintln(buf, "\t\t\tRanges: []gosmi.Range{")
							for _, typeRange := range node.Type.Ranges {
								fmt.Fprintf(buf, "\t\t\t\tgosmi.Range{BaseType: types.BaseType%s, MinValue: %#v, MaxValue: %#v},\n", typeRange.BaseType, typeRange.MinValue, typeRange.MaxValue)
							}
							fmt.Fprintln(buf, "\t\t\t},")
						}
						if node.Type.Units != "" {
							fmt.Fprintf(buf, "\t\t\tUnits: %q,\n", node.Type.Units)
						}
						fmt.Fprintln(buf, "\t\t},")
					} else if node.Kind == types.NodeTable {
						fmt.Fprintf(buf, "\t\tRow: %s.%s,\n", formattedModuleName, formatNodeName(node.GetRow().Name))
					} else if node.Kind == types.NodeRow {
						fmt.Fprintln(buf, "\t\tColumns: []ColumnNode{")
						_, columnOrder := node.GetColumns()
						for _, column := range columnOrder {
							fmt.Fprintf(buf, "\t\t\t%s.%s,\n", formattedModuleName, formatNodeName(column))
						}
						fmt.Fprintln(buf, "\t\t},")
						fmt.Fprintln(buf, "\t\tIndex: []ScalarNode{")
						indices := node.GetIndex()
						for _, index := range indices {
							fmt.Fprintf(buf, "\t\t\t%s.%s,\n", formattedModuleName, formatNodeName(index.Name))
						}
						fmt.Fprintln(buf, "\t\t},")
					} else if node.Kind == types.NodeNotification {
						objects := node.GetNotificationObjects()
						fmt.Fprintln(buf, "\t\tObjects: []ScalarNode{")
						for _, object := range objects {
							fmt.Fprintf(buf, "\t\t\t%s.%s,\n", formattedModuleName, formatNodeName(object.Name))
						}
						fmt.Fprintln(buf, "\t\t},")
					}

					fmt.Fprintln(buf, "\t},")
				}
			}

			fmt.Fprintln(buf, "}")
		}

		formattedOutput, err := format.Source(buf.Bytes())
		if err != nil {
			out.Write(buf.Bytes())
			log.Fatalf("Error generating output: %v\n", err)
		}

		out.Write(formattedOutput)
		if err != nil {
			log.Fatalf("Error writing output: %v\n", err)
		}
	},
}

func formatModuleName(moduleName string) (formattedName string) {
	parts := strings.Split(moduleName, "-")
	for _, part := range parts {
		formattedName += strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return
}

func formatNodeName(nodeName string) (formattedName string) {
	return strings.ToUpper(nodeName[:1]) + nodeName[1:]
}

func init() {
	RootCmd.AddCommand(generateCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// generateCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	generateCmd.Flags().StringVarP(&outFilename, "output", "o", "-", "Output filename, use - for stdout")
	generateCmd.Flags().StringVarP(&packageName, "package", "p", "mibs", "The package for the generated file")
	generateCmd.Flags().StringSliceVarP(&paths, "path", "M", []string{}, "Path(s) to add to MIB search path")
}
