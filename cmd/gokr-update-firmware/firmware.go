package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"context"

	"golang.org/x/sync/errgroup"
)

var (
	userPass = flag.String("github_user_pass",
		"",
		"If non-empty, a user:password string for HTTP basic authentication. See https://github.com/settings/tokens")
)

// Git commit hash of https://github.com/raspberrypi/firmware to take
// firmware files from.
const firmwareRef = "fdb9eafae4b83e553593937eae8e77b0193903c3"

type contentEntry struct {
	Name   string `json:"name"`
	Sha    string `json:"sha"`
	Size   int64  `json:"size"`
	GitURL string `json:"git_url"`
}

func authenticate(req *http.Request) {
	if *userPass != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(*userPass)))
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
		body, _ := ioutil.ReadAll(resp.Body)
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

	if *userPass == "" {
		if fromEnv := os.Getenv("GITHUB_USER") + ":" + os.Getenv("GITHUB_AUTH_TOKEN"); fromEnv != "" {
			*userPass = fromEnv
		}
	}

	var firmwareFiles []string
	for _, pattern := range []string{"*.elf", "*.bin", "*.dat", "overlays/*.dtbo"} {
		files, err := filepath.Glob(pattern)
		if err != nil {
			log.Fatal(err)
		}
		firmwareFiles = append(firmwareFiles, files...)
	}

	// Calculate the git blob hash of each file
	firmwareHashes := make(map[string]string)
	var firmwareHashesLock sync.Mutex
	var eg errgroup.Group
	for _, path := range firmwareFiles {
		path := path // copy
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

			firmwareHashesLock.Lock()
			defer firmwareHashesLock.Unlock()
			firmwareHashes[path] = fmt.Sprintf("%x", hash.Sum(nil))
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		log.Fatal(err)
	}

	// Build a map of all files we want to make sure are up-to-date from GitHub
	// map["start.elf"] = &contentEntry{...} | nil
	filesToCheck := make(map[string]*contentEntry, 0)

	contents, err := githubContents("https://api.github.com/repos/raspberrypi/firmware/contents/boot?ref=" + firmwareRef)
	if err != nil {
		log.Fatal(err)
	}

	// Here we only handle files directly in /boot where we only want to mirror existing files
	for _, path := range firmwareFiles {
		// We handle overlays below
		if filepath.Base(filepath.Dir(path)) == "overlays" {
			continue
		}
		entry, ok := contents[path]
		if ok {
			filesToCheck[path] = &entry
		} else {
			filesToCheck[path] = nil
		}
	}

	contents, err = githubContents("https://api.github.com/repos/raspberrypi/firmware/contents/boot/overlays?ref=" + firmwareRef)
	if err != nil {
		log.Fatal(err)
	}

	// Here we handle /boot/overlays where we want to mirror all files (especially new ones)
	for path := range contents {
		if filepath.Ext(path) != ".dtbo" {
			continue
		}
		downloadPath := filepath.Join("overlays", filepath.Base(path))
		entry, ok := contents[path]
		if ok {
			filesToCheck[downloadPath] = &entry
		} else {
			filesToCheck[downloadPath] = nil
		}
	}

	ctx, canc := context.WithDeadline(context.Background(), time.Now().Add(1*time.Minute))
	defer canc()
	deg, ctx := errgroup.WithContext(ctx)
	deg.SetLimit(5) // number of max concurrent GitHub requests
	for path, githubContent := range filesToCheck {
		githubContent := githubContent

		if githubContent == nil {
			log.Printf("file %q not found on GitHub, obsolete?", path)
			continue
		}

		dirName := filepath.Dir(path)
		if dirName != "" {
			os.MkdirAll(dirName, 0755)
		}

		if got, want := firmwareHashes[path], githubContent.Sha; got != want {
			log.Printf("getting %s (local %s, GitHub %s)", path, got, want)
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
