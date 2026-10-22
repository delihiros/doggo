package project

import (
	"errors"
	"os"
)

// A pin protects one immutable generation from removal. The marker is already
// part of every generation, so pinning does not modify the content or digest.
// Readers acquire it under the publication lease, then release that lease.
type generationPin struct {
	file    *os.File
	project *os.Root
	doggo   *os.Root
}

func lockGenerationMarker(generation *os.Root, exclusive bool) (*os.File, error) {
	parent, err := generation.Open(".")
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	file, err := openGenerationPinFile(parent, generationMarkerName)
	if err != nil {
		return nil, err
	}
	if err := revalidateRootFile(generation, generationMarkerName, file); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := lockGenerationFile(file, exclusive); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	if err := revalidateRootFile(generation, generationMarkerName, file); err != nil {
		return nil, errors.Join(err, closeGenerationMarker(file))
	}
	return file, nil
}

func closeGenerationMarker(file *os.File) error {
	if file == nil {
		return nil
	}
	return errors.Join(unlockGenerationFile(file), file.Close())
}

func (p *generationPin) Close() error {
	if p == nil {
		return nil
	}
	file := p.file
	p.file = nil
	return errors.Join(closeGenerationMarker(file), closeRoot(&p.doggo), closeRoot(&p.project))
}
