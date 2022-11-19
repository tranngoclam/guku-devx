package project

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"devopzilla.com/guku/internal/utils"
	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

func Validate(configDir string) error {
	value := utils.LoadProject(configDir)
	err := value.Validate()
	if err == nil {
		fmt.Println("Looks good 👀👌")
	}
	return err
}

func Discover(configDir string, showTraitDef bool) error {
	instances := utils.LoadInstances(configDir)

	deps := instances[0].Dependencies()

	for _, dep := range deps {
		if strings.Contains(dep.ID(), "traits") {
			ctx := cuecontext.New()
			value := ctx.BuildInstance(dep)

			fieldIter, _ := value.Fields(cue.Definitions(true), cue.Docs(true))

			fmt.Printf("📜 %s\n", dep.ID())
			for fieldIter.Next() {
				traits := fieldIter.Value().LookupPath(cue.ParsePath("$metadata.traits"))
				if traits.Exists() && traits.IsConcrete() {
					fmt.Printf("traits.%s", fieldIter.Selector().String())
					if utils.HasComments(fieldIter.Value()) {
						fmt.Printf("\t%s", utils.GetComments(fieldIter.Value()))
					}
					fmt.Println()
					if showTraitDef {
						fmt.Println(fieldIter.Value())
						fmt.Println()
					}
				}
			}
			fmt.Println()
		}
	}

	return nil
}

func Generate(configDir string) error {
	appPath := path.Join(configDir, "stack.cue")

	os.WriteFile(appPath, []byte(`package main

import (
	"guku.io/devx/v1"
	"guku.io/devx/v1/traits"
)

stack: v1.#Stack & {
	components: {
		app: {
			v1.#Component
			traits.#Workload
			traits.#Exposable
			image: "app:v1"
			ports: [
				{
					port: 8080
				},
			]
			env: {
				PGDB_URL: db.url
			}
			volumes: [
				{
					source: "bla"
					target: "/tmp/bla"
				},
			]
		}
		db: {
			v1.#Component
			traits.#Postgres
			version:    "12.1"
			persistent: true
		}
	}
}
	`), 0700)

	builderPath := path.Join(configDir, "builder.cue")
	os.WriteFile(builderPath, []byte(`package main

import (
	"guku.io/devx/v1"
	"guku.io/devx/v1/traits"
	"guku.io/devx/v1/transformers/compose"
)

builders: v1.#StackBuilder & {
	dev: {
		additionalComponents: {
			observedb: {
				v1.#Component
				traits.#Postgres
				version:    "12.1"
				persistent: true
			}
		}
		flows: [
			v1.#Flow & {
				pipeline: [
					compose.#AddComposeService & {},
				]
			},
			v1.#Flow & {
				pipeline: [
					compose.#AddComposePostgres & {},
				]
			},
		]
	}
}	
	`), 0700)

	return nil
}

func Update(configDir string) error {
	cuemodulePath := path.Join(configDir, "cue.mod", "module.cue")
	data, err := os.ReadFile(cuemodulePath)
	if err != nil {
		return err
	}

	ctx := cuecontext.New()
	cuemodule := ctx.CompileBytes(data)
	if cuemodule.Err() != nil {
		return cuemodule.Err()
	}

	packagesValue := cuemodule.LookupPath(cue.ParsePath("packages"))
	if packagesValue.Err() != nil {
		return packagesValue.Err()
	}

	var packages []string
	err = packagesValue.Decode(&packages)
	if err != nil {
		return err
	}

	for _, pkg := range packages {
		repoURL, repoRevision, repoPath, err := parsePackage(pkg)
		if err != nil {
			return err
		}

		mfs := memfs.New()
		storer := memory.NewStorage()

		// try without auth
		repo, err := git.Clone(storer, mfs, &git.CloneOptions{
			URL:   repoURL,
			Depth: 1,
		})
		if err != nil {
			if err.Error() != "authentication required" {
				return err
			}

			gitUsername := os.Getenv("GIT_USERNAME")
			gitPassword := os.Getenv("GIT_PASSWORD")

			if gitPassword == "" {
				return fmt.Errorf("GIT_PASSWORD and GIT_USERNAME are required to access private repos")
			}

			auth := http.BasicAuth{
				Username: gitUsername,
				Password: gitPassword,
			}

			mfs = memfs.New()
			storer = memory.NewStorage()
			repo, err = git.Clone(storer, mfs, &git.CloneOptions{
				URL:   repoURL,
				Auth:  &auth,
				Depth: 1,
			})
			if err != nil {
				return err
			}
		}

		hash, err := repo.ResolveRevision(plumbing.Revision(repoRevision))

		fmt.Printf("Downloading %s @ %s\n", pkg, hash)

		w, err := repo.Worktree()
		if err != nil {
			return err
		}

		err = w.Checkout(&git.CheckoutOptions{
			Hash: *hash,
		})
		if err != nil {
			return err
		}

		pkgDir := path.Join(configDir, "cue.mod", repoPath)
		err = os.RemoveAll(pkgDir)
		if err != nil {
			return err
		}

		err = utils.FsWalk(mfs, repoPath, func(file string, content []byte) error {
			writePath := path.Join(configDir, "cue.mod", file)

			if err := os.MkdirAll(filepath.Dir(writePath), 0755); err != nil {
				return err
			}

			return os.WriteFile(writePath, content, 0700)
		})

		return err
	}

	return nil
}

func parsePackage(pkg string) (string, string, string, error) {
	parts := strings.SplitN(pkg, "@", 2)
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("No revision specified")
	}
	url := "https://" + parts[0]
	parts = strings.SplitN(parts[1], "/", 2)
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("No path specified")
	}
	revision := parts[0]
	path := parts[1]
	if !strings.HasPrefix(path, "pkg") {
		return "", "", "", fmt.Errorf("Path must start with '/pkg/'")
	}

	return url, revision, path, nil
}

func Init(ctx context.Context, parentDir, module string) error {
	absParentDir, err := filepath.Abs(parentDir)
	if err != nil {
		return err
	}

	modDir := path.Join(absParentDir, "cue.mod")
	if err := os.MkdirAll(modDir, 0755); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}

	modFile := path.Join(modDir, "module.cue")
	if _, err := os.Stat(modFile); err != nil {
		statErr, ok := err.(*os.PathError)
		if !ok {
			return statErr
		}

		contents := fmt.Sprintf(`module: "%s"
packages: [
	"github.com/devopzilla/guku-devx@main/pkg/guku.io",
]
		`, module)
		if err := os.WriteFile(modFile, []byte(contents), 0600); err != nil {
			return err
		}
	}

	if err := os.Mkdir(path.Join(modDir, "pkg"), 0755); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
	}

	return nil
}
