package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

func ListTrackedPullRequests(dataDir string) ([]PullRequestKeyV1, error) {
	root := filepath.Join(dataDir, prsDirName)
	hosts, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list tracked pull requests: %w", err)
	}

	var keys []PullRequestKeyV1
	for _, host := range hosts {
		if !host.IsDir() {
			continue
		}
		owners, err := os.ReadDir(filepath.Join(root, host.Name()))
		if err != nil {
			return nil, fmt.Errorf("list tracked pull requests: %w", err)
		}
		for _, owner := range owners {
			if !owner.IsDir() {
				continue
			}
			repositories, err := os.ReadDir(filepath.Join(root, host.Name(), owner.Name()))
			if err != nil {
				return nil, fmt.Errorf("list tracked pull requests: %w", err)
			}
			for _, repository := range repositories {
				if !repository.IsDir() {
					continue
				}
				numbers, err := os.ReadDir(filepath.Join(root, host.Name(), owner.Name(), repository.Name()))
				if err != nil {
					return nil, fmt.Errorf("list tracked pull requests: %w", err)
				}
				for _, number := range numbers {
					if !number.IsDir() {
						continue
					}
					parsed, err := strconv.Atoi(number.Name())
					if err != nil {
						continue
					}
					key := PullRequestKeyV1{Host: host.Name(), Owner: owner.Name(), Repository: repository.Name(), Number: parsed}
					if key.Validate() != nil {
						continue
					}
					keys = append(keys, key)
				}
			}
		}
	}

	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	return keys, nil
}
