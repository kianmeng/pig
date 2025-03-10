package cmd

import (
	"fmt"
	"os"
	"pig/cli/repo"
	"pig/internal/config"
	"pig/internal/utils"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var (
	repoRegion string
	repoUpdate bool
	repoRemove bool
)

// repoCmd represents the top-level `repo` command
var repoCmd = &cobra.Command{
	Use:     "repo",
	Short:   "Manage Linux Software Repo (apt/dnf)",
	Aliases: []string{"r"},
	GroupID: "pgext",
	Example: `
  typical usage: (Beware that manage repo require sudo/root privilege)
  
  pig repo add                 # add all necessary repo (pgdg + pigsty + node)
  pig repo rm                  # remove yum/atp repo (move existing repo to backup dir)  
  pig repo list                # list current system repo dir and active repos  
  pig repo update              # update yum/apt repo cache (apt update or dnf makecache)
 
  pig repo add -u              # add all necessary repo and update repo cache
  pig repo set -u              # overwrite repo and update repo cache
  pig repo set all -u          # same as above, but remove(backup) old repos first (same as '-r|--remove' option)
  pig repo add all -u          # same as 'pig repo add', also update repo cache 
  pig repo add pigsty pgdg     # add pigsty extension repo + pgdg offical repo
  pig repo add pgsql node      # add os + pgdg postgres repo
  pig repo add infra           # add observability, grafana & prometheus stack, pg bin utils
  pig repo rm                  # remove old repos (move existing repos to ${repodir}/backup)
  pig repo rm pigsty           # remove pigsty repo
  pig repo rm pgsql infra      # remove two repo module: pgsql & infra
`,
}

var repoListCmd = &cobra.Command{
	Use:     "list",
	Short:   "print available repo list",
	Aliases: []string{"l", "ls"},
	Example: `
  pig repo list                # list available repos on current system
  pig repo list all            # list all unfiltered repo raw data
  pig repo list update         # get updated repo data to ~/pig/repo.yml
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return repo.ListRepo()
		} else if args[0] == "all" {
			repo.ListRepoData()
		} else if args[0] == "update" {
			// TODO: implement repo update
			fmt.Println("not implemented yet")
		}
		return nil
	},
}

var repoAddCmd = &cobra.Command{
	Use:     "add",
	Short:   "add new repository",
	Aliases: []string{"a", "append"},
	Example: `
  pig repo add                      # = pig repo add all
  pig repo add all                  # add node,pgsql,infra repo (recommended)
  pig repo add all -u               # add above repo and update repo cache (or: --update)
  pig repo add all -r               # add all repo, remove old repos       (or: --remove)
  pig repo add pigsty --update      # add pigsty extension repo and update repo cache
  pig repo add pgdg --update        # add pgdg official repo and update repo cache
  pig repo add pgsql node --remove  # add os + postgres repo, remove old repos
  pig repo add infra                # add observability, grafana & prometheus stack, pg bin utils

  (Beware that system repo management require sudo / root privilege)

  available repo modules:
  - all      :  pgsql + node + infra (recommended)
    - pigsty :  PostgreSQL Extension Repo (default)
    - pgdg   :  PGDG the Official PostgreSQL Repo (official)
    - node   :  operating system official repo (el/debian/ubuntu)
  - pgsql    :  pigsty + pgdg (all available pg extensions) 
  - extra    :  extra postgres modules, non-free, citus, timescaledb upstream 
  - infra    :  observability, grafana & prometheus stack, pg bin utils
  - local    :  local pigsty repo on 127.0.0.1/pigsty
  - mssql    :  babelfish by wiltondb, MS SQL Server compatible postgres (el + ubuntu)
  - ivory    :  ivorysql, the oracle compatible postgres kernel fork (el only)
  - other    :  pgml, kube, docker, grafana mysql, ...
`,
	// Long: moduleNotice,

	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			args = []string{"all"}
		}
		var repoDir string
		var updateCmd []string
		switch config.OSType {
		case config.DistroEL:
			repoDir, updateCmd = "/etc/yum.repos.d/", []string{"yum", "makecache"}
		case config.DistroDEB:
			repoDir, updateCmd = "/etc/apt/sources.list.d/", []string{"apt-get", "update"}
		default:
			logrus.Errorf("unsupported OS type: %s", config.OSType)
			os.Exit(1)
		}

		if repoRemove {
			logrus.Infof("move existing repo to backup dir")
			if err := repo.BackupRepo(); err != nil {
				logrus.Error(err)
				os.Exit(1)
			}
		}

		if err := repo.AddRepo(repoRegion, args...); err != nil {
			logrus.Error(err)
			os.Exit(1)
		}

		fmt.Printf("======== ls %s\n", repoDir)
		if err := utils.ShellCommand([]string{"ls", "-l", repoDir}); err != nil {
			logrus.Errorf("failed to list repo dir: %s", repoDir)
			os.Exit(1)
		}

		if repoUpdate {
			if err := utils.SudoCommand(updateCmd); err != nil {
				logrus.Error(err)
				os.Exit(1)
			}
		} else {
			logrus.Infof("repo added, consider run: sudo %s", updateCmd)
		}
	},
}

var repoSetCmd = &cobra.Command{
	Use:     "set",
	Short:   "wipe and overwrite repository",
	Aliases: []string{"overwrite"},
	Example: `
  pig repo set all                  # set repo to node,pgsql,infra  (recommended)
  pig repo set all -u               # set repo to above repo and update repo cache (or --update)
  pig repo set pigsty --update      # set repo to pigsty extension repo and update repo cache
  pig repo set pgdg   --update      # set repo to pgdg official repo and update repo cache
  pig repo set infra                # set repo to observability, grafana & prometheus stack, pg bin utils

  (Beware that system repo management require sudo/root privilege)
	`,
	Run: func(cmd *cobra.Command, args []string) {
		repoRemove = true
		repoAddCmd.Run(cmd, args)
	},
}

var repoRmCmd = &cobra.Command{
	Use:     "rm",
	Short:   "remove repository",
	Aliases: []string{"remove"},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			err := repo.BackupRepo()
			if err != nil {
				logrus.Error(err)
				os.Exit(1)
			}
			return
		}
		err := repo.RemoveRepo(args...)
		if err != nil {
			logrus.Error(err)
			os.Exit(1)
		}

		if repoUpdate {
			var updateCmd []string
			if config.OSType == config.DistroEL {
				updateCmd = []string{"yum", "makecache"}
			} else if config.OSType == config.DistroDEB {
				updateCmd = []string{"apt-get", "update"}
			} else {
				logrus.Errorf("unsupported OS type: %s", config.OSType)
				os.Exit(1)
			}

			err = utils.SudoCommand(updateCmd)
			if err != nil {
				logrus.Error(err)
				os.Exit(1)
			}
		}
	},
}

var repoUpdateCmd = &cobra.Command{
	Use:     "update",
	Short:   "update repo cache",
	Aliases: []string{"u"},
	RunE: func(cmd *cobra.Command, args []string) error {
		if config.OSType == config.DistroEL {
			err := utils.SudoCommand([]string{"yum", "makecache"})
			if err != nil {
				logrus.Error(err)
				os.Exit(1)
			}
		} else if config.OSType == config.DistroDEB {
			err := utils.SudoCommand([]string{"apt", "update"})
			if err != nil {
				logrus.Error(err)
				os.Exit(1)
			}
		} else {
			logrus.Errorf("unsupported OS type: %s", config.OSType)
			os.Exit(1)
		}
		return nil
	},
}

var repoStatusCmd = &cobra.Command{
	Use:     "status",
	Short:   "show current repo status",
	Aliases: []string{"s", "st"},
	RunE: func(cmd *cobra.Command, args []string) error {
		return repo.RepoStatus()
	},
}

func init() {
	repoAddCmd.Flags().StringVar(&repoRegion, "region", "", "region code")
	repoAddCmd.Flags().BoolVarP(&repoUpdate, "update", "u", false, "run apt update or dnf makecache")
	repoAddCmd.Flags().BoolVarP(&repoRemove, "remove", "r", false, "remove exisitng repo")
	repoSetCmd.Flags().StringVar(&repoRegion, "region", "", "region code")
	repoSetCmd.Flags().BoolVarP(&repoUpdate, "update", "u", false, "run apt update or dnf makecache")
	repoRmCmd.Flags().BoolVarP(&repoUpdate, "update", "u", false, "run apt update or dnf makecache")

	repoCmd.AddCommand(repoAddCmd)
	repoCmd.AddCommand(repoSetCmd)
	repoCmd.AddCommand(repoRmCmd)
	repoCmd.AddCommand(repoListCmd)
	repoCmd.AddCommand(repoUpdateCmd)
	repoCmd.AddCommand(repoStatusCmd)
}
