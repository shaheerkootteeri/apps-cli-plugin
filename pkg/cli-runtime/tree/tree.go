package tree

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/gosuri/uitable"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/duration"
)

const (
	firstElemPrefix = `├─`
	lastElemPrefix  = `└─`
	indent          = "  "
	pipe            = `│ `
)

var (
	gray  = color.New(color.FgHiBlack)
	red   = color.New(color.FgRed)
	green = color.New(color.FgGreen)
)

type supplychainTemplate string // Needs to move to a different class later
type supplyChain string

// treeView prints object hierarchy to out stream.
func TreeView(out io.Writer, objs objectDirectory, obj unstructured.Unstructured) {
	tbl := uitable.New()
	tbl.Separator = "  "
	tbl.AddRow("NAMESPACE", "NAME", "READY", "REASON", "supply chain template", "supply chain", "AGE")
	treeViewInner("", tbl, objs, obj)
	fmt.Fprintln(color.Output, tbl)
}

func treeViewInner(prefix string, tbl *uitable.Table, objs objectDirectory, obj unstructured.Unstructured) {
	ready, reason := extractStatus(obj)
	supplychainTemplate, supplyChain := extractSupplyChainDetails(obj)
	var readyColor *color.Color
	switch ready {
	case "True":
		readyColor = green
	case "False", "Unknown":
		readyColor = red
	default:
		readyColor = gray
	}
	if ready == "" {
		ready = "-"
	}

	c := obj.GetCreationTimestamp()
	age := duration.HumanDuration(time.Since(c.Time))
	if c.IsZero() {
		age = "<unknown>"
	}

	tbl.AddRow(obj.GetNamespace(), fmt.Sprintf("%s%s/%s",
		gray.Sprint(printPrefix(prefix)),
		obj.GetKind(),
		color.New(color.Bold).Sprint(obj.GetName())),
		readyColor.Sprint(ready),
		readyColor.Sprint(reason),
		readyColor.Sprint(supplychainTemplate),
		readyColor.Sprint(supplyChain),
		age)
	chs := objs.ownedBy(obj.GetUID())
	for i, child := range chs {
		var p string
		switch i {
		case len(chs) - 1:
			p = prefix + lastElemPrefix
		default:
			p = prefix + firstElemPrefix
		}
		treeViewInner(p, tbl, objs, child)
	}
}

func printPrefix(p string) string {
	// this part is hacky af
	if strings.HasSuffix(p, firstElemPrefix) {
		p = strings.Replace(p, firstElemPrefix, pipe, strings.Count(p, firstElemPrefix)-1)
	} else {
		p = strings.ReplaceAll(p, firstElemPrefix, pipe)
	}

	if strings.HasSuffix(p, lastElemPrefix) {
		p = strings.Replace(p, lastElemPrefix, strings.Repeat(" ", len([]rune(lastElemPrefix))), strings.Count(p, lastElemPrefix)-1)
	} else {
		p = strings.ReplaceAll(p, lastElemPrefix, strings.Repeat(" ", len([]rune(lastElemPrefix))))
	}
	return p
}

func extractSupplyChainDetails(obj unstructured.Unstructured) (supplychainTemplate, supplyChain) {
	metadata, ok := obj.Object["metadata"]
	templateValue := ""
	supplyChainName := ""

	if !ok {
		return "", ""
	}
	metadataV, ok := metadata.(map[string]interface{})
	if !ok {
		return "", ""
	}
	labelsF, ok := metadataV["labels"]
	if !ok {
		return "", ""
	}
	labelsV, ok := labelsF.(map[string]interface{})
	if !ok {
		return "", ""
	}

	for key, value := range labelsV {
		// condM, ok := key.(string)
		// if !ok {
		// 	return "", ""
		// }
		// condType, ok := condM["type"].(string)
		// if !ok {
		// 	return "", ""
		// }
		if key == "carto.run/template-kind" {
			templateValue, ok = value.(string)
			if !ok {
				return "", ""
			}
		}
		if key == "carto.run/supply-chain-name" {
			supplyChainName, ok = value.(string)
			if !ok {
				return "", ""
			}
		}

	}

	return supplychainTemplate(templateValue), supplyChain(supplyChainName)
}
