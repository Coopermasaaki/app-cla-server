package controllers

import (
	"encoding/json"
	"fmt"

	"github.com/astaxie/beego"

	"github.com/opensourceways/app-cla-server/dbmodels"
	"github.com/opensourceways/app-cla-server/models"
	"github.com/opensourceways/app-cla-server/pdf"
	"github.com/opensourceways/app-cla-server/util"
)

type LinkController struct {
	baseController
}

func (this *LinkController) Prepare() {
	if this.routerPattern() == "/v1/link/:platform/:org_id/:apply_to" {
		if this.apiReqHeader(headerToken) != "" {
			this.apiPrepare(PermissionIndividualSigner)
		}
	} else {
		this.apiPrepare(PermissionOwnerOfOrg)
	}
}

// @Title Link
// @Description link org and cla
// @Param	body		body 	models.OrgCLA	true		"body for org-repo content"
// @Success 201 {int} models.OrgCLA
// @Failure 403 body is empty
// @router / [post]
func (this *LinkController) Link() {
	doWhat := "create link"
	sendResp := this.newFuncForSendingFailedResp(doWhat)

	pl, fr := this.tokenPayloadBasedOnCodePlatform()
	if fr != nil {
		sendResp(fr)
		return
	}

	input, err := this.fetchPayloadOfCreatingLink()
	if err != nil {
		sendResp(newFailedApiResult(400, errParsingApiBody, err))
		return
	}

	if input.CorpCLA != nil {
		data, fr := this.readInputFile(fileNameOfUploadingOrgSignatue)
		if fr != nil {
			sendResp(fr)
			return
		}
		input.CorpCLA.SetOrgSignature(&data)
	}

	if merr := input.Validate(pdf.GetPDFGenerator().LangSupported()); merr != nil {
		sendResp(parseModelError(merr))
		return
	}

	if r := pl.isOwnerOfOrg(input.OrgID); r != nil {
		sendResp(r)
		return
	}

	filePath := genOrgFileLockPath(input.Platform, input.OrgID, input.RepoID)
	if err := util.CreateLockedFile(filePath); err != nil {
		sendResp(newFailedApiResult(500, errSystemError, err))
		return
	}

	unlock, err := util.Lock(filePath)
	if err != nil {
		sendResp(newFailedApiResult(500, errSystemError, err))
		return
	}
	defer unlock()

	orgRepo := buildOrgRepo(input.Platform, input.OrgID, input.RepoID)
	_, merr := models.GetLinkID(orgRepo)
	if merr == nil {
		sendResp(newFailedApiResult(400, errLinkExists, fmt.Errorf("recreate link")))
		return
	}
	if !merr.IsErrorOf(models.ErrNoLink) {
		sendResp(parseModelError(merr))
		return
	}

	linkID := genLinkID(orgRepo)
	if fr := this.writeLocalFileOfLink(input, linkID); fr != nil {
		sendResp(fr)
		return
	}

	if fr := this.initializeSigning(input, linkID, orgRepo); fr != nil {
		sendResp(fr)
		return
	}

	beego.Info("input.Create")
	if merr := input.Create(linkID, pl.User); merr != nil {
		sendResp(parseModelError(merr))
		return
	}

	this.sendResponse("create org cla successfully", 0)
}

func (this *LinkController) fetchPayloadOfCreatingLink() (*models.LinkCreateOption, error) {
	input := &models.LinkCreateOption{}
	if err := json.Unmarshal([]byte(this.Ctx.Request.FormValue("data")), input); err != nil {
		return nil, fmt.Errorf("invalid input payload: %s", err.Error())
	}
	return input, nil
}

func (this *LinkController) writeLocalFileOfLink(input *models.LinkCreateOption, linkID string) *failedApiResult {
	cla := input.CorpCLA
	if cla != nil {
		path := genCLAFilePath(linkID, dbmodels.ApplyToCorporation, cla.Language)
		if err := cla.SaveCLAAtLocal(path); err != nil {
			return newFailedApiResult(500, errSystemError, err)
		}

		path = genOrgSignatureFilePath(linkID, cla.Language)
		if err := cla.SaveSignatueAtLocal(path); err != nil {
			return newFailedApiResult(500, errSystemError, err)
		}
	}

	cla = input.IndividualCLA
	if cla != nil {
		path := genCLAFilePath(linkID, dbmodels.ApplyToIndividual, cla.Language)
		if err := cla.SaveCLAAtLocal(path); err != nil {
			return newFailedApiResult(500, errSystemError, err)
		}
	}

	return nil
}

func (this *LinkController) initializeSigning(input *models.LinkCreateOption, linkID string, orgRepo *dbmodels.OrgRepo) *failedApiResult {
	var info *dbmodels.CLAInfo

	if input.IndividualCLA != nil {
		info = input.IndividualCLA.GenCLAInfo()
	}
	if merr := models.InitializeIndividualSigning(linkID, info); merr != nil {
		return parseModelError(merr)
	}

	orgInfo := dbmodels.OrgInfo{
		OrgRepo:  *orgRepo,
		OrgEmail: input.OrgEmail,
		OrgAlias: input.OrgAlias,
	}
	info = nil
	if input.CorpCLA != nil {
		info = input.CorpCLA.GenCLAInfo()
	}
	if merr := models.InitializeCorpSigning(linkID, &orgInfo, info); merr != nil {
		return parseModelError(merr)
	}

	return nil
}

// @Title Unlink
// @Description unlink cla
// @Param	uid		path 	string	true		"The uid of binding"
// @Success 204 {string} delete success!
// @Failure 403 uid is empty
// @router /:link_id [delete]
func (this *LinkController) Unlink() {
	doWhat := "unlink"
	sendResp := this.newFuncForSendingFailedResp(doWhat)
	linkID := this.GetString(":link_id")

	pl, fr := this.tokenPayloadBasedOnCodePlatform()
	if fr != nil {
		sendResp(fr)
		return
	}

	if fr := pl.isOwnerOfLink(linkID); fr != nil {
		sendResp(fr)
		return
	}

	if err := models.Unlink(linkID); err != nil {
		sendResp(parseModelError(err))
		return
	}

	this.sendSuccessResp(doWhat + "successfully")
}
