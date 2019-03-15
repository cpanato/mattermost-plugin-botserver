package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
)

type configuration struct {
	AWSAccessKey        string
	AWSSecretKey        string
	AWSRegion           string
	AWSImageId          string
	AWSInstanceType     string
	AWSHostedZoneId     string
	AWSDnsSuffix        string
	AWSSecurityGroup    string
	AWSSubnetId         string
	AllowedUserIds      string
	EnvironmentTemplate string
}

func (c *configuration) Clone() *configuration {
	return &configuration{
		AWSAccessKey: c.AWSAccessKey,
		AWSSecretKey: c.AWSSecretKey,
		AWSRegion:    c.AWSRegion,
	}
}

func (p *Plugin) getConfiguration() *configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &configuration{}
	}

	return p.configuration
}

func (p *Plugin) setConfiguration(configuration *configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

func (p *Plugin) GetAwsConfig() *aws.Config {
	creds := credentials.NewStaticCredentials(
		p.configuration.AWSAccessKey,
		p.configuration.AWSSecretKey,
		"",
	)

	return &aws.Config{
		Credentials: creds,
		Region:      &p.configuration.AWSRegion,
	}
}
