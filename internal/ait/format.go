package ait

import (
	"fmt"
	"strings"
)

func truncateTitle(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

type groupedIssues struct {
	roots            []Issue
	childrenByParent map[string][]Issue
}

func groupIssues(issues []Issue) groupedIssues {
	g := groupedIssues{childrenByParent: make(map[string][]Issue)}
	for _, iss := range issues {
		if iss.ParentID == nil {
			g.roots = append(g.roots, iss)
		} else {
			g.childrenByParent[*iss.ParentID] = append(g.childrenByParent[*iss.ParentID], iss)
		}
	}

	// Sort roots: epics first, then tasks
	epics := make([]Issue, 0)
	tasks := make([]Issue, 0)
	for _, r := range g.roots {
		if r.Type == "epic" {
			epics = append(epics, r)
		} else {
			tasks = append(tasks, r)
		}
	}
	g.roots = append(epics, tasks...)
	return g
}

// childSuffix extracts the ".N" suffix from a child's ID relative to its parent.
func childSuffix(childID, parentID string) string {
	if strings.HasPrefix(childID, parentID) {
		return childID[len(parentID):]
	}
	return childID
}

// FormatHumanList renders a compact tabular view with epics and grouped children.
func FormatHumanList(issues []Issue) string {
	if len(issues) == 0 {
		return ""
	}

	g := groupIssues(issues)
	var b strings.Builder

	for i, root := range g.roots {
		if i > 0 {
			b.WriteString("\n")
		}

		typeLabel := ""
		if root.Type == "epic" {
			typeLabel = "epic"
		}

		b.WriteString(fmt.Sprintf("%-11s  %-45s  %-4s  %-2s  %s\n",
			root.ID,
			truncateTitle(root.Title, 45),
			typeLabel,
			root.Priority,
			root.Status,
		))

		children := g.childrenByParent[root.ID]
		for _, child := range children {
			suffix := childSuffix(child.ID, root.ID)
			b.WriteString(fmt.Sprintf("  %-9s  %-45s        %-2s  %s\n",
				suffix,
				truncateTitle(child.Title, 45),
				child.Priority,
				child.Status,
			))
		}
	}

	return b.String()
}

// FormatTreeList renders a parent-child hierarchy using tree connectors.
func FormatTreeList(issues []Issue) string {
	if len(issues) == 0 {
		return ""
	}

	g := groupIssues(issues)
	var b strings.Builder

	for _, root := range g.roots {
		b.WriteString(fmt.Sprintf("%s  %s  (%s, %s, %s)\n",
			root.ID,
			truncateTitle(root.Title, 45),
			root.Type,
			root.Priority,
			root.Status,
		))

		children := g.childrenByParent[root.ID]
		for j, child := range children {
			connector := "├── "
			if j == len(children)-1 {
				connector = "└── "
			}
			b.WriteString(fmt.Sprintf("%s%s  %s  (%s, %s)\n",
				connector,
				child.ID,
				truncateTitle(child.Title, 45),
				child.Priority,
				child.Status,
			))
		}
	}

	return b.String()
}
