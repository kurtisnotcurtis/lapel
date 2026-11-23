package main

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"lapel/internal/reconcile"
)

// ErrNotAsDesired signals that plan() found resources not matching the manifest.
var ErrNotAsDesired = fmt.Errorf("environment is not as desired")

// printDiff renders a reconcile.DiffResult as an aligned table plus actionable
// hints for any non-desired entry.
func printDiff(res *reconcile.DiffResult) {
	writeDiff(os.Stdout, res)
}

func writeDiff(out io.Writer, res *reconcile.DiffResult) {
	fmt.Fprintf(out, "Reconciling %q (%s)\n", res.Manifest.Name, res.Manifest.APIVersion)

	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "\tKIND\tNAME\tSTATUS\tACTUAL")
	for _, e := range res.Entries {
		status := "OK"
		if !e.Desired {
			status = "DRIFT"
		}
		fmt.Fprintf(w, "\t%s\t%s\t%s\t%s\n", e.Kind, e.Name, status, e.Actual)
	}
	w.Flush()

	if hints := res.NonDesired(); len(hints) > 0 {
		fmt.Fprintln(out, "\nDrift:")
		for _, e := range hints {
			switch e.Kind {
			case "environment":
				fmt.Fprintf(out, "  - %s: distro not installed as desired; run `lapel provision` to build the agent-toolkit baseline.\n", e.Name)
			case "provider":
				fmt.Fprintf(out, "  - %s: endpoint not reachable (%s).\n", e.Name, e.Actual)
			case "workspace":
				fmt.Fprintf(out, "  - %s: %s\n", e.Name, e.Actual)
			}
		}
	} else {
		fmt.Fprintln(out, "\nAll resources match the desired-state manifest.")
	}
}
