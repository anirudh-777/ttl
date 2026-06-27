package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/anirudh-777/ttl/internal/update"
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for and apply newer versions of ttl",
	Long: `Check for and apply newer versions of ttl.

By default, ttl prints a one-line notice on stderr once per day when a
newer release is available. Run '` + "`ttl update`" + `' (or
'` + "`ttl update --check`" + `) to act on it.

Behaviour:
  ttl update           Download the latest release for your OS/arch,
                       verify its SHA256 against SHA256SUMS, and
                       atomically replace the running binary.
  ttl update --check   Just print current vs. latest, no download.
  ttl update --yes     Skip the "are you sure?" prompt.

Disable the background notice with TTL_NO_UPDATE_CHECK=1.`,
	RunE: runUpdate,
}

var (
	updateCheck  bool
	updateYes    bool
	updateTag    string
	updateTo     string
)

func init() {
	updateCmd.Flags().BoolVar(&updateCheck, "check", false,
		"just print current vs latest, don't download")
	updateCmd.Flags().BoolVar(&updateYes, "yes", false,
		"skip the confirmation prompt")
	updateCmd.Flags().StringVar(&updateTag, "version", "latest",
		"specific release tag to install (e.g. v0.4.1)")
	updateCmd.Flags().StringVar(&updateTo, "to", "",
		"destination path (default: replace the running binary)")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	repo := os.Getenv("TTL_REPO")
	if repo == "" {
		repo = update.DefaultRepo
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
	defer cancel()

	if updateCheck {
		res, err := update.Check(ctx, repo, version)
		if err != nil {
			return fmt.Errorf("check: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(),
			"current: %s\nlatest:  %s\n",
			res.Current, res.Latest)
		if res.HasUpdate {
			fmt.Fprintln(cmd.OutOrStdout(), "update available — run `ttl update` to install")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "up to date")
		}
		return nil
	}

	// Pre-check so the prompt shows the user what they're getting.
	res, err := update.Check(ctx, repo, version)
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if !res.HasUpdate && updateTag == "latest" {
		fmt.Fprintf(cmd.OutOrStdout(),
			"ttl %s is already the latest release\n", res.Current)
		return nil
	}
	dest := updateTo
	if dest == "" {
		dest, err = os.Executable()
		if err != nil {
			return err
		}
	}
	tag := updateTag
	if tag == "latest" {
		tag = "v" + res.Latest
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"updating ttl %s -> %s (%s)\n  destination: %s\n",
		res.Current, tag, update.Platform(), dest)

	if !updateYes && !confirmPrompt(cmd) {
		fmt.Fprintln(cmd.OutOrStdout(), "aborted")
		return nil
	}

	if err := update.Apply(ctx, repo, tag, dest); err != nil {
		return fmt.Errorf("apply: %w", err)
	}
	fmt.Fprintf(cmd.OutOrStdout(),
		"updated. run `%s version` to verify.\n", dest)
	return nil
}

// confirmPrompt reads y/N from the command's input. Returns false on
// EOF or anything other than an explicit y / yes.
func confirmPrompt(cmd *cobra.Command) bool {
	in := cmd.InOrStdin()
	if in == nil {
		return false
	}
	fmt.Fprint(cmd.OutOrStdout(), "proceed? [y/N] ")
	var ans string
	_, err := fmt.Fscanln(in, &ans)
	if err != nil {
		return false
	}
	switch ans {
	case "y", "Y", "yes", "Yes", "YES":
		return true
	}
	return false
}
