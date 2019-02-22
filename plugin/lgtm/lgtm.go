package lgtm

import (
	"strings"

	"github.com/fatedier/freebot/pkg/client"
	"github.com/fatedier/freebot/pkg/event"
	"github.com/fatedier/freebot/pkg/log"
	"github.com/fatedier/freebot/pkg/notify"
	"github.com/fatedier/freebot/plugin"
)

var (
	PluginName = "lgtm"
	CmdLGTM    = "lgtm"
)

func init() {
	plugin.Register(PluginName, NewLGTMPlugin)
}

type TargetLabel struct {
	Role         string `json:"role"`
	TargetPrefix string `json:"target_prefix"`
}

type Extra struct {
	BaseLabelPrefix string                         `json:"base_label_prefix"`
	LabelRoles      map[string]map[string][]string `json:"label_roles"`
	TargetLabels    []TargetLabel                  `json:"target_labels"`
}

func (ex *Extra) Complete() {
	if ex.BaseLabelPrefix != "" {
		ex.BaseLabelPrefix = "module"
	}
	if ex.LabelRoles == nil {
		ex.LabelRoles = make(map[string]map[string][]string)
	}
	if ex.TargetLabels == nil {
		ex.TargetLabels = make([]TargetLabel, 0)
	}
}

type LGTMPlugin struct {
	*plugin.BasePlugin

	extra    Extra
	cli      client.ClientInterface
	notifier notify.NotifyInterface
}

func NewLGTMPlugin(cli client.ClientInterface, notifier notify.NotifyInterface, options plugin.PluginOptions) (plugin.Plugin, error) {
	p := &LGTMPlugin{
		cli:      cli,
		notifier: notifier,
	}

	handlerOptions := []plugin.HandlerOptions{
		plugin.HandlerOptions{
			Events:           []string{event.EvPullRequestReview},
			Actions:          []string{event.ActionSubmitted},
			ObjectNeedParams: []int{event.ObjectNeedNumber, event.ObjectNeedSenderUser, event.ObjectNeedReviewState, event.ObjectNeedLabels},
			Handler:          p.handlePullRequestReviewEvent,
		},
		plugin.HandlerOptions{
			Events:           []string{event.EvIssueComment, event.EvPullRequest, event.EvPullRequestReviewComment},
			Actions:          []string{event.ActionCreated},
			ObjectNeedParams: []int{event.ObjectNeedBody, event.ObjectNeedNumber, event.ObjectNeedLabels, event.ObjectNeedCommentAuthor},
			Handler:          p.handleCommentEvent,
		},
	}
	options.Handlers = handlerOptions

	p.BasePlugin = plugin.NewBasePlugin(PluginName, options)

	err := p.UnmarshalTo(&p.extra)
	if err != nil {
		return nil, err
	}
	p.extra.Complete()
	return p, nil
}

func (p *LGTMPlugin) handleCommentEvent(ctx *event.EventContext) (err error) {
	msg, _ := ctx.Object.Body()
	lgtmUser, _ := ctx.Object.CommentAuthor()

	cmds := p.ParseCmdsFromMsg(msg, true)
	for _, cmd := range cmds {
		cmd.Name = p.ParseCmdAlias(cmd.Name)

		if cmd.Name == CmdLGTM {
			return p.handleLGTM(ctx, lgtmUser)
		}
	}
	return
}

func (p *LGTMPlugin) handlePullRequestReviewEvent(ctx *event.EventContext) (err error) {
	reviewState, _ := ctx.Object.ReviewState()
	if reviewState != event.ReviewStateApproved {
		return
	}

	lgtmUser, _ := ctx.Object.SenderUser()
	return p.handleLGTM(ctx, lgtmUser)
}

func (p *LGTMPlugin) handleLGTM(ctx *event.EventContext, lgtmUser string) (err error) {
	number, _ := ctx.Object.Number()
	labels, _ := ctx.Object.Labels()
	targetLabels := make([]string, 0)
	if len(p.extra.LabelRoles) == 0 || len(p.extra.TargetLabels) == 0 {
		return
	}

	for _, label := range labels {
		arrs := strings.Split(label, "/")
		if len(arrs) < 2 {
			continue
		}

		base := arrs[0]
		sub := arrs[1]
		if base != p.extra.BaseLabelPrefix {
			continue
		}

		rolesMap, ok := p.extra.LabelRoles[sub]
		if !ok {
			continue
		}

		for _, t := range p.extra.TargetLabels {
			users, ok := rolesMap[t.Role]
			if !ok {
				continue
			}

			for _, user := range users {
				if lgtmUser == user {
					targetLabels = append(targetLabels, t.TargetPrefix+"/"+sub)
					break
				}
			}
		}
	}

	if len(targetLabels) > 0 {
		err = p.cli.DoOperation(ctx.Ctx, &client.AddLabelOperation{
			Owner:  ctx.Owner,
			Repo:   ctx.Repo,
			Number: number,
			Labels: targetLabels,
		})
		if err != nil {
			return
		}

		log.Debug("[%d] add labels %v", number, targetLabels)
	}
	return
}