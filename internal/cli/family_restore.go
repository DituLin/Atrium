package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/DituLin/Atrium/internal/domain"
	"github.com/DituLin/Atrium/internal/familyarchive"
	"github.com/spf13/cobra"
)

func newFamilyCmd() *cobra.Command {
	root := &cobra.Command{Use: "family", Short: "Recover structured family archives"}
	var o familyarchive.RestoreOptions
	var membersPath string
	restore := &cobra.Command{Use: "restore", Short: "Validate NAS records and create a NEW family-only recovery database", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		file, err := os.Open(membersPath)
		if err != nil {
			return err
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Size() > 65536 {
			return errors.New("members file must be a regular file of at most 64 KiB")
		}
		decoder := json.NewDecoder(io.LimitReader(file, 65537))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&o.Members); err != nil {
			return fmt.Errorf("members must be a JSON array of id/name objects: %w", err)
		}
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			return errors.New("invalid trailing members data")
		}
		if len(o.Members) > 1000 {
			return errors.New("too many member mappings")
		}
		n, err := familyarchive.Restore(cmd.Context(), o)
		if err != nil {
			return err
		}
		return printJSON(cmd.OutOrStdout(), struct {
			Records int                   `json:"records"`
			Members []domain.FamilyMember `json:"members"`
			Output  string                `json:"output"`
			Scope   string                `json:"scope"`
		}{n, o.Members, o.Destination, "family records only; no credentials, photos or original IM replay identities"})
	}}
	restore.Flags().StringVar(&o.Root, "archive", "", "absolute NAS family archive directory")
	restore.Flags().StringVar(&o.Identity, "identity", "", "expected archive marker identity")
	restore.Flags().StringVar(&o.Destination, "out", "", "NEW local SQLite output file; never overwritten")
	restore.Flags().StringVar(&membersPath, "members-file", "", "JSON array mapping archived member IDs to names")
	restore.Flags().BoolVar(&o.AllowLocal, "allow-local", false, "allow a local archive source for tests (output must always be local)")
	root.AddCommand(restore)
	return root
}
