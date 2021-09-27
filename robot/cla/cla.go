package cla

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/astaxie/beego"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/opensourceways/app-cla-server/models"
)

const (
	maxLengthOfSHA    = 8
	signGuideTitle    = "Thanks for your pull request.\n\nThe authors of the following commits have not signed the Contributor License Agreement (CLA):"
	signGuideTitleOld = "Thanks for your pull request. Before we can look at your pull request, you'll need to sign a Contributor License Agreement (CLA)."
)

var (
	CheckCLARe = regexp.MustCompile(`(?mi)^/check-cla\s*$`)
)

type Handler interface {
	Handle(pr PRInfo, labels sets.String) (bool, error)
}

type cla struct {
	getConfig                func() *Configuration
	c                        Client
	signURL                  string
	faqOfCheckingByAuthor    string
	faqOfCheckingByCommitter string
}

func NewCLA(f func() *Configuration, c Client, signURL, faqOfCheckingByAuthor, faqOfCheckingByCommitter string) Handler {
	return cla{
		getConfig:                f,
		c:                        c,
		signURL:                  signURL,
		faqOfCheckingByAuthor:    faqOfCheckingByAuthor,
		faqOfCheckingByCommitter: faqOfCheckingByCommitter,
	}
}

func (cl cla) Handle(pr PRInfo, labels sets.String) (bool, error) {
	cfg := cl.getConfig().CLAFor(pr.Org, pr.Repo)
	if cfg == nil {
		return false, fmt.Errorf("no cla config for this repo:%s/%s", pr.Org, pr.Repo)
	}

	unsigned, err := cl.getPrCommitsAbout(pr, cfg)
	if err != nil {
		return false, err
	}

	faqURL := cl.faqOfCheckingByAuthor
	if cfg.CheckByCommitter {
		faqURL = cl.faqOfCheckingByCommitter
	}
	cl.handle(pr, labels, cfg, unsigned, faqURL)

	return len(unsigned) == 0, nil
}

func (cl cla) handle(pr PRInfo, labels sets.String, cfg *CLARepoConfig, unsigned map[string]string, faqURL string) {
	log := func(msg string, err error) {
		beego.Warning(fmt.Sprintf("%s for %s, err:%s", msg, pr.String(), err.Error()))
	}

	hasCLAYes := labels.Has(cfg.CLALabelYes)
	hasCLANo := labels.Has(cfg.CLALabelNo)

	cl.deleteSignGuide(pr)

	if len(unsigned) == 0 {
		if hasCLANo {
			if err := cl.c.RemovePRLabel(pr, cfg.CLALabelNo); err != nil {
				log(fmt.Sprintf("Could not remove %s label", cfg.CLALabelNo), err)
			}
		}

		if !hasCLAYes {
			if err := cl.c.AddPRLabel(pr, cfg.CLALabelYes); err != nil {
				log(fmt.Sprintf("Could not add %s label", cfg.CLALabelYes), err)
			}
		}
		return
	}

	if hasCLAYes {
		if err := cl.c.RemovePRLabel(pr, cfg.CLALabelYes); err != nil {
			log(fmt.Sprintf("Could not remove %s label", cfg.CLALabelYes), err)
		}
	}

	if !hasCLANo {
		if err := cl.c.AddPRLabel(pr, cfg.CLALabelNo); err != nil {
			log(fmt.Sprintf("Could not add %s label", cfg.CLALabelNo), err)
		}
	}

	s := signGuide(path.Join(cl.signURL, cfg.CLAID), generateUnSignComment(unsigned), faqURL)
	if err := cl.c.CreatePRComment(pr, s); err != nil {
		log("Could not add unsigning comment", err)
	}
}

func (cl cla) getPrCommitsAbout(pr PRInfo, cfg *CLARepoConfig) (map[string]string, error) {
	isSigned := func(email string) bool {
		b, _ := models.IsIndividualSigned(cfg.CLAID, email)
		return b
	}
	return cl.c.GetUnsignedCommits(pr, cfg.CheckByCommitter, isSigned)
}

func (cl cla) deleteSignGuide(pr PRInfo) {
	f := func(s string) bool {
		return strings.HasPrefix(s, signGuideTitle) || strings.HasPrefix(s, signGuideTitleOld)
	}

	cl.c.DeletePRComment(pr, f)
}

func generateUnSignComment(commits map[string]string) string {
	if len(commits) == 0 {
		return ""
	}

	cs := make([]string, 0, len(commits))
	for sha, msg := range commits {
		if len(sha) > maxLengthOfSHA {
			sha = sha[:maxLengthOfSHA]
		}

		cs = append(cs, fmt.Sprintf("**%s** | %s", sha, msg))
	}

	return strings.Join(cs, "\n")
}

func signGuide(signURL, cInfo, faq string) string {
	s := `%s

%s

Please check the [**FAQs**](%s) first.
You can click [**here**](%s) to sign the CLA. After signing the CLA, you must comment "/check-cla" to check the CLA status again.`

	return fmt.Sprintf(s, signGuideTitle, cInfo, faq, signURL)
}