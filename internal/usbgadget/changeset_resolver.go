package usbgadget

import (
	"fmt"

	"github.com/rs/zerolog"
	"github.com/sourcegraph/tf-dag/dag"
)

type ChangeSetResolver struct {
	changeset *ChangeSet
	g         *dag.AcyclicGraph

	changesMap            map[string]*FileChange
	conditionalChangesMap map[string]*FileChange

	orderedChanges            []dag.Vertex
	resolvedChanges           []*FileChange
	additionalResolveRequired bool
}

func (c *ChangeSetResolver) toOrderedChanges() error {
	for key, change := range c.changesMap {
		v := c.g.Add(key)

		for _, dependsOn := range change.DependsOn {
			c.g.Connect(dag.BasicEdge(dependsOn, v))
		}
		for _, dependsOn := range change.resolvedDeps {
			c.g.Connect(dag.BasicEdge(dependsOn, v))
		}
	}

	cycles := c.g.Cycles()
	if len(cycles) > 0 {
		return fmt.Errorf("cycles detected: %v", cycles)
	}

	orderedChanges := c.g.TopologicalOrder()
	c.orderedChanges = orderedChanges
	return nil
}

func (c *ChangeSetResolver) doResolveChanges(initial bool, logger *zerolog.Logger) error {
	resolvedChanges := make([]*FileChange, 0)

	for _, key := range c.orderedChanges {
		change := c.changesMap[key.(string)]
		if change == nil {
			logger.Error().Str("key", key.(string)).Msg("fileChange not found")
			continue
		}

		if !initial {
			change.ResetActionResolution()
		}

		resolvedAction := change.Action(logger)

		resolvedChanges = append(resolvedChanges, change)
		// no need to check the triggers if there's no change
		if resolvedAction == FileChangeResolvedActionDoNothing {
			continue
		}

		if !initial {
			continue
		}

		if change.BeforeChange != nil {
			change.resolvedDeps = append(change.resolvedDeps, change.BeforeChange...)
			c.additionalResolveRequired = true

			// add the dependencies to the changes map
			for _, dep := range change.BeforeChange {
				depChange, ok := c.conditionalChangesMap[dep]
				if !ok {
					return fmt.Errorf("dependency %s not found", dep)
				}

				c.changesMap[dep] = depChange
			}
		}
	}

	c.resolvedChanges = resolvedChanges
	return nil
}

func (c *ChangeSetResolver) resolveChanges(initial bool, logger *zerolog.Logger) error {
	// get the ordered changes
	err := c.toOrderedChanges()
	if err != nil {
		return err
	}

	// resolve the changes
	err = c.doResolveChanges(initial, logger)
	if err != nil {
		return err
	}

	for _, change := range c.resolvedChanges {
		logger.Trace().Stringer("change", change).Msg("resolved change")
	}

	if !c.additionalResolveRequired || !initial {
		return nil
	}

	return c.resolveChanges(false, logger)
}

func (c *ChangeSetResolver) applyChanges(l *zerolog.Logger) error {
	for _, change := range c.resolvedChanges {
		change.ResetActionResolution()

		action := change.Action(l)
		actionStr := FileChangeResolvedActionString[action]

		logger := l.With().Str("action", actionStr).Stringer("change", change).Logger()
		var event *zerolog.Event
		if action == FileChangeResolvedActionDoNothing {
			event = logger.Trace()
		} else {
			event = logger.Debug()
		}

		event.Msg("applying change")

		err := c.changeset.applyChange(change, &logger)
		if err != nil {
			if change.IgnoreErrors {
				logger.Warn().Err(err).Msg("ignoring error")
			} else {
				return err
			}
		}
	}

	return nil
}

func (c *ChangeSetResolver) GetChanges(logger *zerolog.Logger) ([]*FileChange, error) {
	localChanges := c.changeset.Changes
	changesMap := make(map[string]*FileChange)
	conditionalChangesMap := make(map[string]*FileChange)

	// build the map of the changes
	for _, change := range localChanges {
		key := change.Key
		if key == "" {
			key = change.Path
		}

		// remove it from the map first
		if change.When != "" {
			conditionalChangesMap[key] = &change
			continue
		}

		if _, ok := changesMap[key]; ok {
			if changesMap[key].IsSame(&change.RequestedFileChange) {
				continue
			}
			return nil, fmt.Errorf(
				"duplicate change: %s, current: %s, requested: %s",
				key,
				changesMap[key].String(),
				change.String(),
			)
		}

		changesMap[key] = &change
	}

	c.changesMap = changesMap
	c.conditionalChangesMap = conditionalChangesMap

	err := c.resolveChanges(true, logger)
	if err != nil {
		return nil, err
	}

	return c.resolvedChanges, nil
}

func (c *ChangeSetResolver) Apply(logger *zerolog.Logger) error {
	if _, err := c.GetChanges(logger); err != nil {
		return err
	}

	return c.applyChanges(logger)
}
