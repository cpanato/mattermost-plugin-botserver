package main

import (
	"fmt"
	"sync"

	"github.com/pkg/errors"

	"github.com/mattermost/mattermost-server/plugin"
)

type Plugin struct {
	plugin.MattermostPlugin

	spinBotUserID string

	configurationLock sync.RWMutex
	configuration     *configuration
}

func (p *Plugin) OnActivate() error {
	configuration := p.getConfiguration()

	if err := p.IsValid(configuration); err != nil {
		return err
	}

	return p.API.RegisterCommand(getCommand())
}

func (p *Plugin) OnDeactivate() error {
	command := getCommand()
	return p.API.UnregisterCommand("", command.Trigger)
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	var configuration = new(configuration)

	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	p.setConfiguration(configuration)

	return nil
}

func (p *Plugin) IsValid(configuration *configuration) error {
	if configuration.AWSAccessKey == "" {
		return fmt.Errorf("Must have an AWS Access Id")
	}

	if configuration.AWSSecretKey == "" {
		return fmt.Errorf("Must have an AWS Secret Key")
	}

	if configuration.AWSRegion == "" {
		return fmt.Errorf("Must have an AWS Region")
	}

	if configuration.AWSImageId == "" {
		return fmt.Errorf("Must have an AWS Image Id")
	}

	if configuration.AWSInstanceType == "" {
		return fmt.Errorf("Must have an AWS Instance Type")
	}

	if configuration.AWSHostedZoneId == "" {
		return fmt.Errorf("Must have an AWS Hosted Zone Id")
	}

	if configuration.AWSDnsSuffix == "" {
		return fmt.Errorf("Must have a DNS Name")
	}

	if configuration.AWSSecurityGroup == "" {
		return fmt.Errorf("Must have an AWS Security Group")
	}

	if configuration.EnvironmentTemplate == "" {
		return fmt.Errorf("Must have the Template to spin the server")
	}

	return nil
}
