package main

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/route53"

	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/plugin"
)

const (
	SPIN_ICON_URL = "https://icon-icons.com/icons2/1371/PNG/512/robot01_90832.png"
	SPIN_USERNAME = "Bot Server"
	BOT_KV_KEY    = "_BOTSERVER_"
)

func getCommand() *model.Command {
	return &model.Command{
		Trigger:          "bot-server",
		DisplayName:      "Bot Server",
		Description:      "Spin your Test environment",
		AutoComplete:     true,
		AutoCompleteDesc: "Available commands: spin, destroy, help",
		AutoCompleteHint: "[command]",
	}
}

func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	split := strings.Fields(args.Command)
	command := split[0]
	parameters := []string{}
	action := ""
	if len(split) > 1 {
		action = strings.TrimSpace(split[1])
	}
	if len(split) > 2 {
		parameters = split[2:]
	}

	if command != "/bot-server" {
		return &model.CommandResponse{}, nil
	}

	err := p.checkIfUserCanUseCommand(args.UserId)
	if err != nil {
		return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, err.Error()), nil
	}

	channel, errr := p.API.GetChannel(args.ChannelId)
	if err != nil {
		p.API.LogError("Error getting the Channel to validate the command", "user_id", args.UserId, "channel_id", args.ChannelId, "err", errr.Message)
		return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Error getting the Channel to validate the command."), nil
	}

	userDM := fmt.Sprintf("%s__%s", args.UserId, args.UserId)
	if channel.Type != model.CHANNEL_DIRECT || channel.Name != userDM {
		return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "You need to run the command in your Direct Channel."), nil
	}

	if action == "" {
		return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Missing command, please run `/bot-server help` to check all commands available."), nil
	}

	if action == "help" {
		msg := "run:\n/bot-server spin PACKAGE_URL INSTANCE_NAME to spin a new test server\n/bot-server destroy INSTANCE_NAME to destroy the test server"
		return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, msg), nil
	}

	if action != "spin" && action != "destroy" && action != "help" {
		return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Invalid command, please run `/bot-server help` to check all commands available. Action="+action), nil
	}

	switch action {
	case "spin":
		if len(parameters) != 2 {
			return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Missing the PACKAGE_URL or INSTANCE_NAME to deploy the app"), nil
		}

		if !isValidUrl(parameters[0]) {
			return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "PACKAGE_URL is not valid."), nil
		}

		checkInstance := p.getInstanceId(args.UserId)
		if checkInstance != "" {
			return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "You already have a test server running, if you want another please destroy this first."), nil
		}

		instanceID, publicIP, internalIP := p.spinServer(args.UserId, args.ChannelId, parameters)
		if instanceID == "" {
			return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Error creating the Environment. Please check if your sysadmin the configuration"), nil
		}

		err := p.storeInstanceId(args.UserId, instanceID)
		if err != nil {
			return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Error saving the instance Id, here is to you use when call the destroy command instanceID="+instanceID), nil
		}

		p.sendMessageSpinServer(c, args, publicIP, internalIP)
	case "destroy":
		if len(parameters) == 0 {
			return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Missing the name of the server to destroy"), nil
		}
		info, err := p.deleteInstanceId(args.UserId, parameters[0])
		if err != nil {
			return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, err.Message), nil
		}
		if info == "" {
			return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Nothing to destroy."), nil
		}
		return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, "Instance "+info+" destroyed."), nil
	case "help":
	default:
		msg := "run:\n/bot-server spin [flags] to spin a new test server\n/bot-server destroy to destroy the test server"
		return getCommandResponse(model.COMMAND_RESPONSE_TYPE_EPHEMERAL, msg), nil
	}

	return &model.CommandResponse{}, nil
}

func (p *Plugin) spinServer(userId, channelId string, parameters []string) (instanceID, domainName string, internalIP string) {
	svc := ec2.New(session.New(), p.GetAwsConfig())

	setupScript := p.configuration.EnvironmentTemplate

	releasePath := parameters[0]
	instanceName := parameters[1]
	dnsName := fmt.Sprintf("%s.%s", instanceName, p.configuration.AWSDnsSuffix)
	setupScript = strings.Replace(setupScript, "RELEASE_PATH", releasePath, -1)
	setupScript = strings.Replace(setupScript, "DNS_NAME", dnsName, -1)
	bsdata := []byte(setupScript)
	sdata := base64.StdEncoding.EncodeToString(bsdata)

	var one int64 = 1
	params := &ec2.RunInstancesInput{
		ImageId:          &p.configuration.AWSImageId,
		MaxCount:         &one,
		MinCount:         &one,
		InstanceType:     &p.configuration.AWSInstanceType,
		UserData:         &sdata,
		SecurityGroupIds: []*string{&p.configuration.AWSSecurityGroup},
		SubnetId:         &p.configuration.AWSSubnetId,
	}

	var userName string
	user, errUser := p.API.GetUser(userId)
	if errUser != nil {
		userName = userId
	}
	userName = user.Nickname

	resp, err := svc.RunInstances(params)
	if err != nil {
		p.API.LogError("We could not create the aws resource", "user_id", userId, "err", err.Error())
		return "", "", ""
	}
	instanceId := resp.Instances[0].InstanceId

	msg := "Waiting for the Server come up"
	p.sendEphemeralMessage(msg, channelId, userId)

	time.Sleep(time.Minute * 2)
	externalIP, internalIP := p.getIPsForInstance(*instanceId)

	p.API.LogDebug("AWS INFO", "InstanceId", instanceId, "PublicIP", externalIP)

	// Add tags to the created instance
	_, errtag := svc.CreateTags(&ec2.CreateTagsInput{
		Resources: []*string{instanceId},
		Tags: []*ec2.Tag{
			{
				Key:   aws.String("Name"),
				Value: aws.String(dnsName),
			},
			{
				Key:   aws.String("Created"),
				Value: aws.String(time.Now().Format("2006-01-02/15:04:05")),
			},
			{
				Key:   aws.String("UserName"),
				Value: aws.String(userName),
			},
			{
				Key:   aws.String("CreatedBy"),
				Value: aws.String("BotServer"),
			},
		},
	})
	if errtag != nil {
		p.API.LogError("Could not create tags for instance", "user_id", userId, "InstanceId", instanceId, "err", err.Error())
	}

	// Set the DNS
	domainName, err = p.updateRoute53Subdomain(dnsName, externalIP, "CREATE")
	if err != nil {
		p.API.LogError("Unable to set up S3 subdomain using the aws public name", "user_id", userId, "InstanceId", instanceId, "PublicIp", externalIP, "err", err.Error())
		return *instanceId, externalIP, internalIP
	}
	msg = "Setting the DNS"
	p.sendEphemeralMessage(msg, channelId, userId)

	return *instanceId, domainName, internalIP
}

func (p *Plugin) sendMessageSpinServer(c *plugin.Context, args *model.CommandArgs, publicIP string, internalIP string) {
	config := p.API.GetConfig()
	siteURLPort := *config.ServiceSettings.ListenAddress
	action1 := &model.PostAction{
		Name: "Destroy Test Server",
		Type: model.POST_ACTION_TYPE_BUTTON,
		Integration: &model.PostActionIntegration{
			Context: map[string]interface{}{
				"action":     "destroy",
				"public_dns": publicIP,
				"user_id":    args.UserId,
			},
			URL: fmt.Sprintf("http://localhost%v/plugins/%v/destroy", siteURLPort, PluginId),
		},
	}
	sa1 := &model.SlackAttachment{
		Text: "Your Test server was created. Access here: https://" + publicIP,
		Actions: []*model.PostAction{
			action1,
		},
	}
	attch := &model.SlackAttachment{
		Text: "Internal IP: " + internalIP,
	}
	attachments := make([]*model.SlackAttachment, 0)
	attachments = append(attachments, sa1)
	attachments = append(attachments, attch)

	spinPost := &model.Post{
		Message:   "",
		ChannelId: args.ChannelId,
		UserId:    args.UserId,
		Props: model.StringInterface{
			"attachments":       attachments,
			"override_username": SPIN_USERNAME,
			"override_icon_url": SPIN_ICON_URL,
			"from_webhook":      "true",
		},
	}

	if _, err := p.API.CreatePost(spinPost); err != nil {
		p.API.LogError(
			"We could not create the spin test server post",
			"user_id", args.UserId,
			"err", err.Error(),
		)
	}
	p.API.LogDebug(
		"Posted new test server",
		"user_id", args.UserId,
		"publicIP", publicIP,
		"internalIP", internalIP,
	)

}

func (p *Plugin) storeInstanceId(userID, instanceId string) error {
	key := fmt.Sprintf("%s%s", BOT_KV_KEY, userID)
	err := p.API.KVSet(key, []byte(instanceId))
	if err != nil {
		return fmt.Errorf("Encountered error saving instanceId mapping")
	}
	return nil
}

func (p *Plugin) getInstanceId(userID string) string {
	key := fmt.Sprintf("%s%s", BOT_KV_KEY, userID)
	instanceId, _ := p.API.KVGet(key)
	return string(instanceId)
}

func (p *Plugin) deleteInstanceId(userID, PublicIP string) (info string, err *model.AppError) {
	key := fmt.Sprintf("%s%s", BOT_KV_KEY, userID)
	instanceId, err := p.API.KVGet(key)
	if err != nil {
		return "", err
	}
	if instanceId == nil {
		return "", nil
	}

	err = p.API.KVDelete(key)
	if err != nil {
		return "", err
	}

	svc := ec2.New(session.New(), p.GetAwsConfig())

	instance := string(instanceId)
	params := &ec2.TerminateInstancesInput{
		InstanceIds: []*string{
			&instance,
		},
	}

	//TODO not return and try again and alert the user
	_, errr := svc.TerminateInstances(params)
	if errr != nil {
		return instance, model.NewAppError("TerminateInstances", "", nil, errr.Error(), -1)
	}

	// Remove route53 entry
	//TODO not return and try again and alert the user
	_, errr = p.updateRoute53Subdomain(instance, PublicIP, "DELETE")
	if err != nil {
		return instance, model.NewAppError("updateRoute53Subdomain Delete", "", nil, errr.Error(), -1)
	}

	return instance, nil
}

func (p *Plugin) checkIfUserCanUseCommand(userID string) error {

	if userID == "" {
		return fmt.Errorf("Need a user id")
	}

	hasPremissions := false
	AllowedUserIds := strings.Split(p.configuration.AllowedUserIds, ",")
	for _, allowedUserId := range AllowedUserIds {
		if allowedUserId == userID {
			hasPremissions = true
			break
		}
	}

	if !hasPremissions {
		return fmt.Errorf("You don't have permissions to use this command. Please talk with your SysAdmin.")
	}

	return nil
}

func (p *Plugin) updateRoute53Subdomain(name, target, action string) (string, error) {
	svc := route53.New(session.New(), p.GetAwsConfig())

	dnsName := name
	targetServer := target
	if action == "DELETE" {
		targetServer, _ = p.getIPsForInstance(name)
		dnsName = target
	}

	params := &route53.ChangeResourceRecordSetsInput{
		ChangeBatch: &route53.ChangeBatch{
			Changes: []*route53.Change{
				{
					Action: aws.String(action),
					ResourceRecordSet: &route53.ResourceRecordSet{
						Name: aws.String(dnsName),
						TTL:  aws.Int64(30),
						Type: aws.String("A"),
						ResourceRecords: []*route53.ResourceRecord{
							{
								Value: aws.String(targetServer),
							},
						},
					},
				},
			},
		},
		HostedZoneId: &p.configuration.AWSHostedZoneId,
	}

	_, err := svc.ChangeResourceRecordSets(params)
	if err != nil {
		p.API.LogDebug("Error removing the DNS", "err", err.Error())
		return "", err
	}

	return dnsName, nil
}

func (p *Plugin) getIPsForInstance(instance string) (publicIP string, privateIP string) {
	svc := ec2.New(session.New(), p.GetAwsConfig())
	params := &ec2.DescribeInstancesInput{
		InstanceIds: []*string{
			&instance,
		},
	}
	resp, err := svc.DescribeInstances(params)
	if err != nil {
		p.API.LogError("Problem getting instance", "Instance", instance, "err", err.Error())
		return "", ""
	}

	return *resp.Reservations[0].Instances[0].PublicIpAddress, *resp.Reservations[0].Instances[0].PrivateIpAddress
}

func getCommandResponse(responseType, text string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: responseType,
		Text:         text,
		Username:     SPIN_USERNAME,
		IconURL:      SPIN_ICON_URL,
		Type:         model.POST_DEFAULT,
	}
}

func (p *Plugin) sendEphemeralMessage(msg, channelId, userId string) {
	ephemeralPost := &model.Post{
		Message:   msg,
		ChannelId: channelId,
		UserId:    userId,
		Props: model.StringInterface{
			"override_username": SPIN_USERNAME,
			"override_icon_url": SPIN_ICON_URL,
			"from_webhook":      "true",
		},
	}

	p.API.LogDebug("Will send an ephemeralPost", "msg", msg)

	p.API.SendEphemeralPost(userId, ephemeralPost)
}

func isValidUrl(toTest string) bool {
	_, err := url.ParseRequestURI(toTest)
	if err != nil {
		return false
	} else {
		return true
	}
}
