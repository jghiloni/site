package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/alecthomas/kong"
	"github.com/bluesky-social/indigo/api/atproto"
	"github.com/bluesky-social/indigo/api/bsky"
	lexutil "github.com/bluesky-social/indigo/lex/util"
	"github.com/bluesky-social/indigo/util"
	"github.com/bluesky-social/indigo/xrpc"
	"github.com/mitchellh/pointerstructure"
)

type hugoListPost struct {
	Path        string
	Slug        string
	Title       string
	Date        time.Time
	ExpiryDate  time.Time
	PublishDate time.Time
	Draft       bool
	URL         string
	Kind        string
	Section     string
}

type cliArgs struct {
	PdsUrl       string `short:"p" default:"https://bsky.social" help:"the PDS URL for the bluesky account"`
	Username     string `short:"u" required:"true" help:"the bluesky account username"`
	AppPassword  string `short:"w" required:"true" env:"APP_PASSWORD" help:"an app password for the bluesky account"`
	HugoListCSV  string `short:"f" optional:"true" hidden:"true"`
	DryRun       bool   `default:"false" hidden:"true"`
	SimulatePush bool   `default:"false" hidden:"true"`
}

const postPrefix = "A new post has been published to joshghiloni.me! Read "
const postSuffix = " and reply to this skeet to comment on it and join the conversation"
const frontMatterBarrier = "+++"

func main() {
	log.SetFlags(log.LstdFlags | log.Llongfile)
	var args cliArgs
	kong.Parse(&args)

	publishedPosts, err := getPosts(args)
	if err != nil {
		log.Fatal(err)
	}

	err = skeetPosts(publishedPosts, args)
	if err != nil {
		log.Fatal(err)
	}
}

func getPosts(args cliArgs) (posts []hugoListPost, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
			return
		}
	}()

	var reader *csv.Reader
	if args.HugoListCSV != "" {
		var f *os.File
		f, err = os.Open(args.HugoListCSV)
		if err != nil {
			return
		}
		defer f.Close()

		reader = csv.NewReader(f)
	} else {
		reader = csv.NewReader(os.Stdin)
	}

	reader.FieldsPerRecord = 10

	// skip the header row
	_, err = reader.Read()
	if err != nil {
		return nil, err
	}

	posts = []hugoListPost{}
	var lines [][]string
	lines, err = reader.ReadAll()
	if err != nil {
		return
	}

	for _, line := range lines {
		posts = append(posts, hugoListPost{
			Path:        line[0],
			Slug:        line[1],
			Title:       line[2],
			Date:        mustTime(line[3]),
			ExpiryDate:  mustTime(line[4]),
			PublishDate: mustTime(line[5]),
			Draft:       mustBool(line[6]),
			URL:         line[7],
			Kind:        line[8],
			Section:     line[9],
		})
	}

	return
}

func mustTime(timeStr string) time.Time {
	if timeStr == "" {
		return time.Time{}
	}

	time, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		panic(err)
	}

	return time
}

func mustBool(boolStr string) bool {
	b, err := strconv.ParseBool(boolStr)
	if err != nil {
		panic(err)
	}

	return b
}

func skeetPosts(posts []hugoListPost, args cliArgs) error {
	filtered := slices.DeleteFunc(posts, func(post hugoListPost) bool {
		fm, _, err := readPost(post)
		if err != nil {
			return false
		}

		params, _ := pointerstructure.Get(fm, "/params/skeet")
		return params != nil
	})

	xrpcClient := &xrpc.Client{
		Client:    util.RobustHTTPClient(),
		Host:      args.PdsUrl,
		UserAgent: ptr("joshghiloni.me/git-hooks"),
	}

	if !args.DryRun {
		output, err := atproto.ServerCreateSession(context.Background(), xrpcClient, &atproto.ServerCreateSession_Input{
			Identifier: args.Username,
			Password:   args.AppPassword,
		})
		if err != nil {
			return err
		}

		xrpcClient.Auth = &xrpc.AuthInfo{
			AccessJwt:  output.AccessJwt,
			RefreshJwt: output.RefreshJwt,
			Handle:     output.Handle,
			Did:        output.Did,
		}
	}

	for _, post := range filtered {
		skeet := bsky.FeedPost{
			CreatedAt: time.Now().Format(time.RFC3339),
			Text:      fmt.Sprintf("%s%s%s", postPrefix, post.Title, postSuffix),
			Facets: []*bsky.RichtextFacet{
				{
					Index: &bsky.RichtextFacet_ByteSlice{
						ByteStart: int64(len(postPrefix)),
						ByteEnd:   int64(len(postPrefix) + len(post.Title)),
					},
					Features: []*bsky.RichtextFacet_Features_Elem{
						{
							RichtextFacet_Link: &bsky.RichtextFacet_Link{
								Uri: post.URL,
							},
						},
					},
				},
			},
		}

		output := &atproto.RepoCreateRecord_Output{
			Uri: "at://did:plc:sampledid/samplekey",
		}

		var err error
		if !args.DryRun {
			output, err = atproto.RepoCreateRecord(context.Background(), xrpcClient, &atproto.RepoCreateRecord_Input{
				Collection: "app.bsky.feed.post",
				Repo:       xrpcClient.Auth.Did,
				Record: &lexutil.LexiconTypeDecoder{
					Val: &skeet,
				},
			})
			if err != nil {
				return err
			}
		}

		if args.DryRun {
			json.NewEncoder(os.Stdout).Encode(skeet)
			if !args.SimulatePush {
				continue
			}
		}

		if err = updatePost(post, output.Uri); err != nil {
			return err
		}
	}

	return nil
}

func readPost(post hugoListPost) (frontMatter any, contents string, err error) {
	var postFile string
	postFile, err = filepath.Abs(filepath.Join("..", post.Path))
	if err != nil {
		return
	}

	var fileContents []byte
	fileContents, err = os.ReadFile(postFile)
	if err != nil {
		return
	}

	scanner := bufio.NewScanner(bytes.NewBuffer(fileContents))
	foundTopMarker := false
	foundBottomMarker := false
	frontMatterLines := []string{}
	contentLines := []string{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == frontMatterBarrier {
			if !foundTopMarker {
				foundTopMarker = true
				continue
			}

			if !foundBottomMarker {
				foundBottomMarker = true
				continue
			}
		}

		if foundTopMarker && !foundBottomMarker {
			frontMatterLines = append(frontMatterLines, line)
			continue
		}

		contentLines = append(contentLines, line)
	}

	contents = strings.Join(contentLines, "\n")
	_, err = toml.NewDecoder(strings.NewReader(strings.Join(frontMatterLines, "\n"))).Decode(&frontMatter)

	return
}

func updatePost(post hugoListPost, skeetURI string) error {
	frontMatter, postContent, err := readPost(post)
	if err != nil {
		return err
	}

	var foundSkeet any
	foundSkeet, _ = pointerstructure.Get(frontMatter, "/params/skeet")

	if foundSkeet != nil {
		return errors.New("skeet already set")
	}

	frontMatter, err = pointerstructure.Set(frontMatter, "/params", map[string]string{
		"skeet": skeetURI,
	})
	if err != nil {
		return err
	}

	postFile, err := filepath.Abs(filepath.Join("..", post.Path))
	if err != nil {
		return err
	}

	pf, err := os.Create(postFile)
	if err != nil {
		return err
	}
	defer pf.Close()

	fmt.Fprintln(pf, frontMatterBarrier)
	if err = toml.NewEncoder(pf).Encode(frontMatter); err != nil {
		return err
	}
	fmt.Fprintln(pf)
	fmt.Fprintln(pf, frontMatterBarrier)
	fmt.Fprintln(pf, postContent)
	return nil
}

func ptr[T any](val T) *T {
	return &val
}
