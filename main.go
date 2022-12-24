package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/google/go-github/github"
	"github.com/pkg/errors"
	"github.com/shurcooL/githubv4"
	"github.com/urfave/cli/v2"
	"golang.org/x/oauth2"
)

func newClientv4(apiUrl, token string) (*githubv4.Client, error) {
	tc := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))
	baseURL, err := url.Parse(apiUrl)
	if err != nil {
		return nil, err
	}
	baseURL.Path = "/api/graphql"

	return githubv4.NewEnterpriseClient(baseURL.String(), tc), nil
}

func newClientv3(apiUrl, token string) (*github.Client, error) {
	tc := oauth2.NewClient(context.Background(), oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	))

	baseURL, err := url.Parse(apiUrl)
	if err != nil {
		return nil, err
	}
	baseURL.Path = "/api/v3"

	gh, err := github.NewEnterpriseClient(baseURL.String(), baseURL.String(), tc)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create GitHub client")
	}

	return gh, err
}

func v3(conf Config) {
	c, err := newClientv3(conf.URL, conf.Token)
	if err != nil {
		panic("failed to create client: " + err.Error())
	}

	stop := false

	next := 0

	var printf func(msg string, parts ...any)
	if conf.Org == "" {
		printf = printGen("v3:all:repos")
	} else {
		printf = printGen(fmt.Sprintf("v3:%s:repos", conf.Org))
	}

	count := 0
	for !stop {

		var (
			repos []*github.Repository
			resp  *github.Response
			err   error
		)

		if conf.Org != "" {
			repos, resp, err = c.Repositories.ListByOrg(context.Background(), conf.Org, &github.RepositoryListByOrgOptions{
				ListOptions: github.ListOptions{
					Page:    next,
					PerPage: 100,
				},
			})
		} else {
			repos, resp, err = c.Repositories.List(context.Background(), "", &github.RepositoryListOptions{
				ListOptions: github.ListOptions{
					Page:    next,
					PerPage: 100,
				},
			})
		}

		if err != nil {
			printf("List failure: %s\n", err.Error())
		}

		count += len(repos)
		printf("Fetched 100\n")
		printf("Page %d\n", next)
		printf("Total %d\n", count)

		next = resp.NextPage

		for _, r := range repos {
			name := r.GetFullName()
			if name == "" {
				printf("Problem repo: %s %s\n", r.GetHTMLURL(), r.GetCloneURL())
				continue
			} else if len(name) < strings.Index(name, "/")+1 {
				printf("Problem repo: %d %s %s %s\n", r.ID, r.GetFullName(), r.GetHTMLURL(), r.GetCloneURL())
			}
		}
	}
}

type ReposByOrg struct {
	Organization struct {
		Login        string
		Repositories struct {
			TotalCount githubv4.Int
			Edges      []struct {
				Cursor githubv4.String
				Node   struct {
					Repo
				}
			}
		} `graphql:"repositories(first: 100, after:$after isFork: false, ownerAffiliations: OWNER, orderBy: {field: CREATED_AT, direction: DESC})"`
	} `graphql:"organization(login:$login)"`
}

type Repo struct {
	Id            string
	DatabaseId    int
	NameWithOwner string
	Description   string
	Url           string
	IsPrivate     bool
	IsFork        bool
	IsArchived    bool
	IsLocked      bool
	IsDisabled    bool
	ForkCount     int
}
type ReposByViewer struct {
	Viewer struct {
		Login        string
		Repositories struct {
			TotalCount githubv4.Int
			Edges      []struct {
				Cursor githubv4.String
				Node   struct {
					Repo
				}
			}
		} `graphql:"repositories(first: 100, after:$after isFork: false, ownerAffiliations: OWNER, orderBy: {field: CREATED_AT, direction: DESC})"`
	}
}

type OrgsByViewer struct {
	Viewer struct {
		Login         string
		Organizations struct {
			TotalCount githubv4.Int
			Edges      []struct {
				Cursor githubv4.String
				Node   struct {
					Id          string
					DatabaseId  int
					Name        string
					Description string
				}
			}
		} `graphql:"organizations(first: 100, after:$after)"`
	}
}

type Config struct {
	Token string
	URL   string
	Org   string
}

func printGen(prefix string) func(string, ...any) {
	return func(format string, parts ...any) {
		msg := fmt.Sprintf("[%s] %s", prefix, format)
		log.Printf(msg, parts...)
	}
}

func loadOrgsv4(client *githubv4.Client) []string {
	printf := printGen("v4:orgs")
	ctx := context.Background()
	stop := false
	total := 0
	after := (*githubv4.String)(nil)
	var viewer OrgsByViewer
	orgs := []string{}
	for !stop {
		vars := map[string]any{
			//"username": githubv4.String("milton"),
			"after": after,
		}
		err := client.Query(ctx, &viewer, vars)
		if err != nil {
			printf("failed to execute graphql query: %s\n", err.Error())
			return orgs
		}
		size := len(viewer.Viewer.Organizations.Edges)
		total += size
		printf("Fetched %d\n", size)
		for _, o := range viewer.Viewer.Organizations.Edges {
			orgs = append(orgs, string(o.Node.Name))
		}

		after = &viewer.Viewer.Organizations.Edges[size-1].Cursor
		left := int(viewer.Viewer.Organizations.TotalCount) - total
		if left == 0 {
			stop = true
		}
		printf("Next Cursor: %s\n", string(*after))
		printf("Total: %d\n", total)
		printf("Left: %d\n", left)
	}
	return orgs

}

func v4(c Config) {
	client, err := newClientv4(c.URL, c.Token)
	if err != nil {
		panic(err)
	}

	orgs := []string{}
	if c.Org == "" {
		orgs = loadOrgsv4(client)
	} else {
		orgs = append(orgs, c.Org)
	}

	orgRepos := make(map[string][]Repo)
	for _, org := range orgs {
		fmt.Printf("----------- %s -----------", org)
		orgRepos[org] = ReposForOrg(client, org)
	}
}

func ReposForOrg(client *githubv4.Client, org string) []Repo {
	printf := printGen(fmt.Sprintf("v4:org:%s:repos", org))
	ctx := context.Background()
	stop := false
	total := 0
	after := (*githubv4.String)(nil)
	var orgViewer ReposByOrg

	repos := make([]Repo, 0)

	for !stop {
		vars := map[string]any{
			"login": githubv4.String(org),
			"after": after,
		}
		err := client.Query(ctx, &orgViewer, vars)
		if err != nil {
			printf("failed to execute graphql query: %s\n", err.Error())
			return nil
		}

		size := len(orgViewer.Organization.Repositories.Edges)
		total += size
		printf("Fetched %d\n", size)
		for _, r := range orgViewer.Organization.Repositories.Edges {
			repos = append(repos, r.Node.Repo)
		}

		after = &orgViewer.Organization.Repositories.Edges[size-1].Cursor
		left := int(orgViewer.Organization.Repositories.TotalCount) - total
		printf("Next Cursor: %s\n", string(*after))
		printf("Total: %d\n", total)
		printf("Left: %d\n", left)
		if left == 0 {
			stop = true
		}
	}

	return repos
}

func main() {
	app := cli.App{
		Commands: []*cli.Command{
			{
				Name: "list",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:     "api-version",
						Usage:    "which GitHub api version to use 3 (REST API) or 4 (GraphQL)",
						Required: false,
						Value:    3,
					},
					&cli.StringFlag{
						Name:     "token",
						Usage:    "GitHub PAT with scopes ['read:org', 'repo:read']",
						Required: true,
						EnvVars:  []string{"GITHUB_TOKEN"},
					},
					&cli.StringFlag{
						Name:     "url",
						Usage:    "GitHub Enterprise url (GitHub.com not supported)",
						Required: true,
						EnvVars:  []string{"GITHUB_URL"},
					},
					&cli.StringFlag{
						Name: "org",
					},
				},
				Action: func(ctx *cli.Context) error {
					switch ctx.Int("api-version") {
					case 3:
						{
							fmt.Println("---- Using API Client v3 ----")
							token := ctx.String("token")
							apiUrl := ctx.String("url")
							org := ctx.String("org")
							v3(Config{URL: apiUrl, Token: token, Org: org})
						}
					case 4:
						{
							fmt.Println("---- Using API Client v4 ----")
							token := ctx.String("token")
							apiUrl := ctx.String("url")
							org := ctx.String("org")
							v4(Config{URL: apiUrl, Token: token, Org: org})
						}
					default:
						return fmt.Errorf("unknown api version")
					}
					return nil
				},
			},
		}}
	if err := app.Run(os.Args); err != nil {
		panic(err)
	}
}
