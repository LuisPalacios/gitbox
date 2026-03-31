package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/credential"
	"github.com/LuisPalacios/gitbox/pkg/provider"
	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   "repo",
	Short: "Manage repos within sources",
}

// --- repo list ---

var repoListSource string

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List repos",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if jsonOutput {
			if repoListSource != "" {
				repos, err := cfg.ListRepos(repoListSource)
				if err != nil {
					return err
				}
				data, _ := json.MarshalIndent(repos, "", "    ")
				fmt.Fprintln(os.Stdout, string(data))
				return nil
			}
			data, _ := json.MarshalIndent(cfg.Sources, "", "    ")
			fmt.Fprintln(os.Stdout, string(data))
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintf(w, "SOURCE\tREPO\tCREDENTIAL\tID_FOLDER\tCLONE_FOLDER\n")
		fmt.Fprintf(w, "──────\t────\t──────────\t─────────\t────────────\n")
		for srcKey, src := range cfg.Sources {
			if repoListSource != "" && srcKey != repoListSource {
				continue
			}
			acct, _ := cfg.GetAccountByKey(src.Account)
			for repoKey, repo := range src.Repos {
				cred := repo.EffectiveCredentialType(&acct)
				credDisplay := cred
				if repo.CredentialType == "" {
					credDisplay = cred + " (default)"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", srcKey, repoKey, credDisplay, repo.IdFolder, repo.CloneFolder)
			}
		}
		return w.Flush()
	},
}

// --- repo show ---

var repoShowCmd = &cobra.Command{
	Use:   "show <source-key> <org/repo>",
	Short: "Show repo details",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		repos, err := cfg.ListRepos(args[0])
		if err != nil {
			return err
		}

		repo, ok := repos[args[1]]
		if !ok {
			return fmt.Errorf("repo %q not found in source %q", args[1], args[0])
		}

		data, _ := json.MarshalIndent(repo, "", "    ")
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	},
}

// --- repo add ---

var (
	repoCredType    string
	repoIdFolder    string
	repoCloneFolder string
)

var repoAddCmd = &cobra.Command{
	Use:   "add <source-key> <org/repo>",
	Short: "Add a repo to a source",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		repo := config.Repo{
			CredentialType: repoCredType,
			IdFolder:       repoIdFolder,
			CloneFolder:    repoCloneFolder,
		}

		if err := cfg.AddRepo(args[0], args[1], repo); err != nil {
			return err
		}

		return saveConfig(cfg)
	},
}

// --- repo update ---

var repoUpdateCmd = &cobra.Command{
	Use:   "update <source-key> <org/repo>",
	Short: "Update a repo",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		repos, err := cfg.ListRepos(args[0])
		if err != nil {
			return err
		}
		repo, ok := repos[args[1]]
		if !ok {
			return fmt.Errorf("repo %q not found in source %q", args[1], args[0])
		}

		if cmd.Flags().Changed("credential-type") {
			repo.CredentialType = repoCredType
		}
		if cmd.Flags().Changed("id-folder") {
			repo.IdFolder = repoIdFolder
		}
		if cmd.Flags().Changed("clone-folder") {
			repo.CloneFolder = repoCloneFolder
		}

		if err := cfg.UpdateRepo(args[0], args[1], repo); err != nil {
			return err
		}

		return saveConfig(cfg)
	},
}

// --- repo delete ---

var repoDeleteCmd = &cobra.Command{
	Use:   "delete <source-key> <org/repo>",
	Short: "Delete a repo from a source",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if err := cfg.DeleteRepo(args[0], args[1]); err != nil {
			return err
		}

		return saveConfig(cfg)
	},
}

// --- repo create ---

var (
	repoCreateOwner   string
	repoCreateDesc    string
	repoCreatePrivate bool
	repoCreatePublic  bool
)

var repoCreateCmd = &cobra.Command{
	Use:   "create <account-key> <repo-name>",
	Short: "Create a new repository on the provider",
	Long:  "Creates a new repository on the remote provider via its API. The repo is created as private by default.",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		accountKey := args[0]
		repoName := args[1]

		cfg, err := loadConfig()
		if err != nil {
			return err
		}

		acct, ok := cfg.GetAccountByKey(accountKey)
		if !ok {
			return fmt.Errorf("account %q not found", accountKey)
		}

		token, _, err := credential.ResolveAPIToken(acct, accountKey)
		if err != nil {
			return fmt.Errorf("resolving token: %w", err)
		}

		prov, err := provider.ByName(acct.Provider)
		if err != nil {
			return err
		}

		rc, ok := prov.(provider.RepoCreator)
		if !ok {
			return fmt.Errorf("provider %q does not support repository creation", acct.Provider)
		}

		private := !repoCreatePublic
		owner := repoCreateOwner

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := rc.CreateRepo(ctx, acct.URL, token, acct.Username, owner, repoName, repoCreateDesc, private); err != nil {
			return fmt.Errorf("creating repo: %w", err)
		}

		visibility := "private"
		if !private {
			visibility = "public"
		}
		location := acct.Username
		if owner != "" {
			location = owner
		}
		fmt.Printf("Created %s repo %s/%s on %s\n", visibility, location, repoName, acct.URL)
		return nil
	},
}

func init() {
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoShowCmd)
	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoUpdateCmd)
	repoCmd.AddCommand(repoDeleteCmd)
	repoCmd.AddCommand(repoCreateCmd)

	repoListCmd.Flags().StringVar(&repoListSource, "source", "", "filter by source")

	repoAddCmd.Flags().StringVar(&repoCredType, "credential-type", "", "credential type (gcm, ssh, token) — omit to use account default")
	repoAddCmd.Flags().StringVar(&repoIdFolder, "id-folder", "", "override 2nd level folder (org)")
	repoAddCmd.Flags().StringVar(&repoCloneFolder, "clone-folder", "", "override 3rd level folder (clone name)")

	repoUpdateCmd.Flags().StringVar(&repoCredType, "credential-type", "", "credential type")
	repoUpdateCmd.Flags().StringVar(&repoIdFolder, "id-folder", "", "override 2nd level folder")
	repoUpdateCmd.Flags().StringVar(&repoCloneFolder, "clone-folder", "", "override 3rd level folder")

	repoCreateCmd.Flags().StringVar(&repoCreateOwner, "owner", "", "organization/group to create repo under (default: personal)")
	repoCreateCmd.Flags().StringVar(&repoCreateDesc, "description", "", "repository description")
	repoCreateCmd.Flags().BoolVar(&repoCreatePublic, "public", false, "create as public (default: private)")
}
