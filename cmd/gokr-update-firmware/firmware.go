package main

import (
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"context"

	"golang.org/x/sync/errgroup"
)

var (
	githubToken = flag.String("github_token",
		"",
		"If non-empty, a GitHub access token for HTTP authentication. See https://github.com/settings/tokens")
)

// Git commit hash of https://github.com/raspberrypi/firmware to take
// firmware files from.
const firmwareRef = "3885768a3aad70f42a9d9a85b550046b6b9d83fe"

type contentEntry struct {
	Name   string `json:"name"`
	Sha    string `json:"sha"`
	Size   int64  `json:"size"`
	GitURL string `json:"git_url"`
}

func authenticate(req *http.Request) {
	if *githubToken != "" {
		req.Header.Set("Authorization", "Bearer "+*githubToken)
	}
}

func githubContents(url string) (map[string]contentEntry, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	authenticate(req)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code: got %d, want %d (body: %s)", got, want, string(body))
	}
	var contents []contentEntry
	if err := json.NewDecoder(resp.Body).Decode(&contents); err != nil {
		return nil, err
	}
	result := make(map[string]contentEntry, len(contents))
	for _, c := range contents {
		result[c.Name] = c
	}
	return result, nil
}

func main() {
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if *githubToken == "" {
		if fromEnv := os.Getenv("GITHUB_AUTH_TOKEN"); fromEnv != "" {
			*githubToken = fromEnv
		}
	}

	var firmwareFiles []string
	for _, pattern := range []string{"*.elf", "*.bin", "*.dat"} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			log.Fatal(err)
		}
		firmwareFiles = append(firmwareFiles, files...)
	}

	// Calculate the git blob hash of each file
	firmwareHashes := make([]string, len(firmwareFiles))
	var eg errgroup.Group
	for idx, path := range firmwareFiles {
		idx, path := idx, path // copy
		eg.Go(func() error {
			hash := sha1.New()
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil {
				return err
			}
			if _, err := fmt.Fprintf(hash, "blob %d\x00", st.Size()); err != nil {
				return err
			}
			if _, err := io.Copy(hash, f); err != nil {
				return err
			}
			firmwareHashes[idx] = fmt.Sprintf("%x", hash.Sum(nil))
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		log.Fatal(err)
	}

	contents, err := githubContents("https://api.github.com/repos/raspberrypi/firmware/contents/boot?ref=" + firmwareRef)
	if err != nil {
		log.Fatal(err)
	}

	ctx, canc := context.WithDeadline(context.Background(), time.Now().Add(1*time.Minute))
	defer canc()
	deg, ctx := errgroup.WithContext(ctx)
	deg.SetLimit(5) // number of max concurrent GitHub requests
	for idx, path := range firmwareFiles {
		fn := filepath.Base(path)
		githubContent, ok := contents[fn]
		if !ok {
			log.Printf("file %q not found on GitHub, obsolete?", fn)
			continue
		}
		if got, want := firmwareHashes[idx], githubContent.Sha; got != want {
			log.Printf("getting %s (local %s, GitHub %s)", fn, got, want)
			path := path // copy
			deg.Go(func() error {
				req, err := http.NewRequest(http.MethodGet, githubContent.GitURL, nil)
				if err != nil {
					return err
				}
				authenticate(req)
				req.Header.Set("Accept", "application/vnd.github.v3.raw")

				resp, err := http.DefaultClient.Do(req.WithContext(ctx))
				if err != nil {
					return err
				}

				f, err := os.Create(path)
				if err != nil {
					return err
				}
				defer f.Close()

				if _, err := io.Copy(f, resp.Body); err != nil {
					return err
				}

				return f.Close()
			})
		}
	}
	if err := deg.Wait(); err != nil {
		log.Fatal(err)
	}
}
