package format

import (
	"sort"
	"strings"
)

// renderTree draws an ASCII file tree from sorted slash paths.
//
//	myrepo/
//	|-- docs/
//	|   `-- guide.md
//	`-- main.go
func renderTree(root string, paths []string) string {
	type node struct {
		name     string
		children map[string]*node
		isDir    bool
	}
	newNode := func(name string, isDir bool) *node {
		return &node{name: name, children: map[string]*node{}, isDir: isDir}
	}
	top := newNode(root+"/", true)
	for _, p := range paths {
		parts := strings.Split(p, "/")
		cur := top
		for i, part := range parts {
			isDir := i < len(parts)-1
			child, ok := cur.children[part]
			if !ok {
				child = newNode(part, isDir)
				cur.children[part] = child
			}
			cur = child
		}
	}

	var b strings.Builder
	b.WriteString(top.name + "\n")
	var walkNode func(n *node, prefix string)
	walkNode = func(n *node, prefix string) {
		names := make([]string, 0, len(n.children))
		for name := range n.children {
			names = append(names, name)
		}
		// Directories first, then files, both ascending - matches how humans
		// read trees; stable and deterministic.
		sort.Slice(names, func(i, j int) bool {
			a, bn := n.children[names[i]], n.children[names[j]]
			if a.isDir != bn.isDir {
				return a.isDir
			}
			return a.name < bn.name
		})
		for i, name := range names {
			child := n.children[name]
			connector, childPrefix := "|-- ", prefix+"|   "
			if i == len(names)-1 {
				connector, childPrefix = "`-- ", prefix+"    "
			}
			label := child.name
			if child.isDir {
				label += "/"
			}
			b.WriteString(prefix + connector + label + "\n")
			if child.isDir {
				walkNode(child, childPrefix)
			}
		}
	}
	walkNode(top, "")
	return b.String()
}
