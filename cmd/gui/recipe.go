//go:build !gitboxserver

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/LuisPalacios/gitbox/pkg/config"
	"github.com/LuisPalacios/gitbox/pkg/recipe"
	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"
)

// The native dialog picks the file. A bound call never accepts a filesystem
// path from recipe content or from a remote window.
var openRecipeFile = wailsrt.OpenFileDialog
var saveRecipeFile = wailsrt.SaveFileDialog
var errRecipeChanged = errors.New("recipe.changed")
var recipeFilter = []wailsrt.FileFilter{{DisplayName: "JSON", Pattern: "*.json"}}

type RecipeFile struct {
	Document    string              `json:"document"`
	Slots       []string            `json:"slots"`
	NewAccounts []recipe.AccountRef `json:"newAccounts"`
}

func (a *App) recipeConfig() (*config.Config, error) {
	if a.remoteWindow() {
		return nil, errors.New("recipe.localOnly")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.saveRefused(); err != nil {
		return nil, errors.New("recipe.configUnavailable")
	}
	return a.cfg.Clone()
}

func (a *App) PreviewRecipeExport() (string, error) {
	cfg, err := a.recipeConfig()
	if err != nil {
		return "", err
	}
	r, err := recipe.Export(cfg)
	if err != nil {
		return "", errors.New("recipe.invalid")
	}
	doc, err := r.Document()
	if err != nil {
		return "", errors.New("recipe.invalid")
	}
	return doc, nil
}

func (a *App) ExportRecipe(reviewed string) (bool, error) {
	doc, err := a.PreviewRecipeExport()
	if err != nil {
		return false, err
	}
	if doc != reviewed {
		return false, errRecipeChanged
	}
	path, err := saveRecipeFile(a.ctx, wailsrt.SaveDialogOptions{DefaultFilename: "gitbox-recipe.json", Filters: recipeFilter})
	if err != nil {
		return false, errors.New("recipe.fileError")
	}
	if path == "" {
		return false, nil
	}
	if target, err := os.Stat(path); err == nil {
		if active, err := os.Stat(a.cfgPath); err == nil && os.SameFile(target, active) {
			return false, errors.New("recipe.fileError")
		}
	}
	// Avoid damaging a previously saved recipe if a write fails.
	f, err := os.CreateTemp(filepath.Dir(path), ".gitbox-recipe-*")
	if err != nil {
		return false, errors.New("recipe.fileError")
	}
	defer os.Remove(f.Name())
	_, writeErr := io.WriteString(f, doc)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil || os.Rename(f.Name(), path) != nil {
		return false, errors.New("recipe.fileError")
	}
	return true, nil
}

func (a *App) OpenRecipeImport() (RecipeFile, error) {
	cfg, err := a.recipeConfig()
	if err != nil {
		return RecipeFile{}, err
	}
	path, err := openRecipeFile(a.ctx, wailsrt.OpenDialogOptions{Filters: recipeFilter})
	if err != nil {
		return RecipeFile{}, errors.New("recipe.fileError")
	}
	if path == "" {
		return RecipeFile{}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return RecipeFile{}, errors.New("recipe.fileError")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > recipe.MaxSize {
		return RecipeFile{}, errors.New("recipe.invalid")
	}
	data, err := io.ReadAll(io.LimitReader(file, recipe.MaxSize+1))
	if err != nil {
		return RecipeFile{}, errors.New("recipe.fileError")
	}
	return inspectRecipeFile(cfg, string(data))
}

func inspectRecipeFile(cfg *config.Config, doc string) (RecipeFile, error) {
	r, err := recipe.Decode(doc)
	if err != nil {
		return RecipeFile{}, errors.New("recipe.invalid")
	}
	missing, err := recipe.NewAccounts(cfg, r)
	if err != nil {
		return RecipeFile{}, errors.New("recipe.ambiguous")
	}
	// Return the allowlisted shape even when a file had unusual formatting.
	canonical, err := r.Document()
	if err != nil {
		return RecipeFile{}, errors.New("recipe.invalid")
	}
	out := RecipeFile{Document: canonical, NewAccounts: missing}
	for _, root := range r.Roots {
		out.Slots = append(out.Slots, root.Slot)
	}
	return out, nil
}

func planRecipe(cfg *config.Config, doc string, options recipe.Options) (recipe.ImportPlan, error) {
	r, err := recipe.Decode(doc)
	if err != nil {
		return recipe.ImportPlan{}, errors.New("recipe.invalid")
	}
	plan, err := recipe.Plan(cfg, r, options)
	if err != nil {
		return recipe.ImportPlan{}, errors.New("recipe.mappingError")
	}
	return plan, nil
}

func (a *App) PreviewRecipeImport(doc string, options recipe.Options) (recipe.ImportPlan, error) {
	cfg, err := a.recipeConfig()
	if err != nil {
		return recipe.ImportPlan{}, err
	}
	return planRecipe(cfg, doc, options)
}

func (a *App) ApplyRecipeImport(doc string, options recipe.Options, reviewed string) error {
	if a.remoteWindow() {
		return errors.New("recipe.localOnly")
	}
	_, err := a.commitConfig(func(cfg *config.Config) error {
		plan, err := planRecipe(cfg, doc, options)
		if err != nil || plan.Digest != reviewed || plan.Collisions > 0 {
			return errRecipeChanged
		}
		return recipe.Apply(cfg, plan)
	})
	if errors.Is(err, errRecipeChanged) || isConfigChanged(err) {
		return errRecipeChanged
	}
	if err != nil {
		return errors.New("recipe.applyError")
	}
	return nil
}
