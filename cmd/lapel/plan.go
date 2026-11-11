package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"lapel/internal/reconcile"
)

// ErrNotAsDesired signals that plan() found resources not matching the manifest.
var ErrNotAsDesired = fmt.Errorf("environment is not as desired")

// printDiff renders a reconcile.DiffResult as an aligned table plus actionable
// hints for any non-desired entry, then exits with a non-zero status when used
// from main (see planCmd).
func printDiff(res *reconcile.DiffResult) {
	fmt.Printf("Reconciling %q (%s)\n", res.Manifest.Name, res.Manifest.APIVersion)

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "\tKIND\tNAME\tSTATUS\tACTUAL")
	for _, e := range res.Entries {
		desired := "OK"
		if !e.Desired {
			desired = "DRIFT"
		}
		fmt.Fprintf(w, "\t%s\t%s\t%s\t%s\n", desired, e.Kind, e.Name, e.Actual)
	}
	w.Flush()

	if hints := res.NonDesired(); len(hints) > 0 {
		fmt.Println("\nDrift:")
		for _, e := range hints {
			switch e.Kind {
			case "environment":
				fmt.Printf("  - %s: distro not installed as desired; run `lapel provision` to build the agent-toolkit baseline.\n", e.Name)
			case "provider":
				fmt.Printf("  - %s: endpoint not reachable (%s).\n", e.Name, e.Actual)
			case "workspace":
				fmt.Printf("  - %s: %s\n", e.Name, e.Actual)
			}
		}
	} else {
		fmt.Println("\nAll resources match the desired-state manifest.")
	}
}
