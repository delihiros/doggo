package cli

import (
	toolchaindeps "doggo/internal/toolchain/deps"
	"fmt"
	"strings"
)

func (app *App) runDepsCommand() error {
	return app.runNamedSubcommand(defaultDepsSubcommandRegistry())
}

func (app *App) depsService() (*toolchaindeps.Service, error) {
	if app.services.Deps == nil {
		return nil, fmt.Errorf("deps service is not configured")
	}
	return app.services.Deps, nil
}

func (app *App) runDepsAddSubcommand() error {
	service, err := app.depsService()
	if err != nil {
		return err
	}
	if len(app.args) < 3 {
		return fmt.Errorf("add command requires a dependency source")
	}
	source := app.args[2]
	if strings.HasPrefix(strings.TrimSpace(source), "pkg:") {
		resolvedSource, resolvedPkg, err := service.ResolveSource(source, "")
		if err != nil {
			return err
		}
		fmt.Printf("Resolved %s -> %s (%s@%s)\n", source, resolvedSource, resolvedPkg.Name, resolvedPkg.Version)
		source = resolvedSource
	}
	return service.Add(source)
}

func (app *App) runDepsRemoveSubcommand() error {
	service, err := app.depsService()
	if err != nil {
		return err
	}
	if len(app.args) < 3 {
		return fmt.Errorf("remove command requires a dependency source")
	}
	return service.Remove(app.args[2])
}

func (app *App) runDepsListSubcommand() error {
	service, err := app.depsService()
	if err != nil {
		return err
	}
	deps, err := service.List()
	if err != nil {
		return err
	}
	if len(deps) == 0 {
		fmt.Println("No dependencies")
		return nil
	}
	for _, dep := range deps {
		fmt.Println(dep)
	}
	return nil
}

func (app *App) runDepsOutdatedSubcommand() error {
	service, err := app.depsService()
	if err != nil {
		return err
	}
	statuses, err := service.OutdatedStatus()
	if err != nil {
		return err
	}
	if len(statuses) == 0 {
		fmt.Println("No dependencies")
		return nil
	}

	checkErrors := 0
	outdatedCount := 0
	for _, status := range statuses {
		fmt.Println(formatOutdatedStatusLine(status))
		if status.CheckError != "" {
			checkErrors++
			continue
		}
		if status.Outdated {
			outdatedCount++
		}
	}

	if checkErrors > 0 {
		return fmt.Errorf("failed to check %d dependencies", checkErrors)
	}
	if outdatedCount == 0 {
		fmt.Println("All dependencies are up to date")
	}
	return nil
}

func (app *App) runDepsUpdateSubcommand() error {
	service, err := app.depsService()
	if err != nil {
		return err
	}
	updateAll := false
	targetSource := ""
	switch len(app.args) {
	case 2:
		updateAll = true
	case 3:
		if app.args[2] == "--all" {
			updateAll = true
		} else {
			targetSource = app.args[2]
		}
	default:
		return fmt.Errorf("update command accepts either --all or a single dependency source")
	}

	_, results, err := service.UpdateDependencies(targetSource, updateAll)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Println("No dependencies")
		return nil
	}

	updatedCount := 0
	for _, result := range results {
		if result.Updated {
			updatedCount++
		}
		fmt.Println(formatDependencyUpdateLine(result))
	}
	if updatedCount == 0 {
		fmt.Println("All selected dependencies are already up to date")
	}
	return nil
}

func (app *App) runDepsAuditSubcommand() error {
	service, err := app.depsService()
	if err != nil {
		return err
	}
	if len(app.args) != 2 {
		return fmt.Errorf("audit command does not accept extra arguments")
	}
	summary, err := service.Audit()
	if err != nil {
		return err
	}
	findings := append([]toolchaindeps.DependencyAuditFinding{}, summary.Blocking...)
	findings = append(findings, summary.NonBlocking...)
	if len(findings) == 0 {
		fmt.Println("No audit findings")
		return nil
	}
	for _, finding := range findings {
		fmt.Println(formatDependencyAuditFindingLine(finding))
	}
	if len(summary.Blocking) > 0 {
		return fmt.Errorf("dependency audit found %d high-severity issue(s) (%d total)", len(summary.Blocking), len(findings))
	}
	fmt.Printf("dependency audit: %d non-blocking issue(s)\n", len(findings))
	return nil
}
