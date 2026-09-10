package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"backend/internal/masterdata"
)

const publicCacheControl = "public, max-age=60"

type Handler struct {
	svc *masterdata.Service
}

func New(svc *masterdata.Service) (*Handler, error) {
	if svc == nil {
		return nil, masterdata.ErrStoreRequired
	}
	return &Handler{svc: svc}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/master-data/categories", h.listCategories)
	mux.HandleFunc("GET /v1/master-data/categories/{categoryId}/form", h.currentForm)
	mux.HandleFunc("GET /v1/master-data/categories/{categoryId}/schemas/{version}/form", h.versionedForm)
}

func (h *Handler) listCategories(w http.ResponseWriter, r *http.Request) {
	locale, ok := parseLocaleParam(w, r)
	if !ok {
		return
	}
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	cats, err := h.svc.ListPublishedCategories(r.Context())
	if err != nil {
		writeMasterDataError(w, err)
		return
	}
	out := make([]categoryDTO, 0, len(cats))
	for _, c := range cats {
		item := categoryDTO{
			ID:               c.ID.String(),
			Code:             c.Code,
			ParentID:         parentIDString(c.ParentID),
			Label:            localizedCategoryLabel(c.Labels, locale),
			HasPublishedForm: c.HasPublishedForm,
		}
		if c.HasPublishedForm {
			v := c.SchemaVersion
			item.SchemaVersion = &v
		}
		out = append(out, item)
	}
	writePublicJSON(w, categoriesResponse{Categories: out})
}

func (h *Handler) currentForm(w http.ResponseWriter, r *http.Request) {
	locale, ok := parseLocaleParam(w, r)
	if !ok {
		return
	}
	categoryID, ok := parseCategoryID(w, r)
	if !ok {
		return
	}
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	form, err := h.svc.ResolvePublishedForm(r.Context(), categoryID)
	if err != nil {
		writeMasterDataError(w, err)
		return
	}
	writePublicJSON(w, toFormDTO(form, locale))
}

func (h *Handler) versionedForm(w http.ResponseWriter, r *http.Request) {
	locale, ok := parseLocaleParam(w, r)
	if !ok {
		return
	}
	categoryID, ok := parseCategoryID(w, r)
	if !ok {
		return
	}
	version, ok := parseSchemaVersion(w, r)
	if !ok {
		return
	}
	if h.svc == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable")
		return
	}
	form, err := h.svc.ResolvePublishedFormAt(r.Context(), categoryID, version)
	if err != nil {
		writeMasterDataError(w, err)
		return
	}
	writePublicJSON(w, toFormDTO(form, locale))
}

func parseLocaleParam(w http.ResponseWriter, r *http.Request) (masterdata.Locale, bool) {
	locale, err := masterdata.ParseLocale(r.URL.Query().Get("locale"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return "", false
	}
	return locale, true
}

func parseCategoryID(w http.ResponseWriter, r *http.Request) (masterdata.ID, bool) {
	id, err := masterdata.ParseID(r.PathValue("categoryId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return masterdata.ID{}, false
	}
	return id, true
}

func parseSchemaVersion(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := strings.TrimSpace(r.PathValue("version"))
	if raw == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return 0, false
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version < 1 {
		writeError(w, http.StatusBadRequest, "bad_request")
		return 0, false
	}
	return version, true
}

type categoriesResponse struct {
	Categories []categoryDTO `json:"categories"`
}

type categoryDTO struct {
	ID               string  `json:"id"`
	Code             string  `json:"code"`
	ParentID         *string `json:"parentId"`
	Label            string  `json:"label"`
	HasPublishedForm bool    `json:"hasPublishedForm"`
	SchemaVersion    *int    `json:"schemaVersion,omitempty"`
}

type formDTO struct {
	CategoryID    string     `json:"categoryId"`
	CategoryCode  string     `json:"categoryCode"`
	SchemaVersion int        `json:"schemaVersion"`
	Label         string     `json:"label"`
	Fields        []fieldDTO `json:"fields"`
}

type fieldDTO struct {
	Code        string         `json:"code"`
	Label       string         `json:"label"`
	HelpText    *string        `json:"helpText,omitempty"`
	ValueType   string         `json:"valueType"`
	Required    bool           `json:"required"`
	Constraints map[string]any `json:"constraints"`
	Options     []optionDTO    `json:"options,omitempty"`
}

type optionDTO struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func toFormDTO(form masterdata.FormDefinition, locale masterdata.Locale) formDTO {
	fields := make([]fieldDTO, 0, len(form.Fields))
	for _, f := range form.Fields {
		item := fieldDTO{
			Code:        f.Code,
			Label:       localizedAttributeLabel(f.Labels, locale),
			HelpText:    localizedHelpText(f.Labels, locale),
			ValueType:   string(f.ValueType),
			Required:    f.Required,
			Constraints: f.Constraints,
		}
		if item.Constraints == nil {
			item.Constraints = map[string]any{}
		}
		if f.ValueType == masterdata.ValueTypeEnum {
			opts := make([]optionDTO, 0, len(f.Options))
			for _, o := range f.Options {
				opts = append(opts, optionDTO{
					Code:  o.Code,
					Label: localizedOptionLabel(o.Labels, locale),
				})
			}
			item.Options = opts
		}
		fields = append(fields, item)
	}
	return formDTO{
		CategoryID:    form.CategoryID.String(),
		CategoryCode:  form.CategoryCode,
		SchemaVersion: form.SchemaVersion,
		Label:         localizedCategoryLabel(form.CategoryLabels, locale),
		Fields:        fields,
	}
}

func localizedCategoryLabel(labels []masterdata.CategoryLabel, locale masterdata.Locale) string {
	var fallback string
	for _, l := range labels {
		if l.Locale == locale {
			return l.Label
		}
		if l.Locale == masterdata.LocaleTR {
			fallback = l.Label
		}
	}
	return fallback
}

func localizedAttributeLabel(labels []masterdata.AttributeLabel, locale masterdata.Locale) string {
	var fallback string
	for _, l := range labels {
		if l.Locale == locale {
			return l.Label
		}
		if l.Locale == masterdata.LocaleTR {
			fallback = l.Label
		}
	}
	return fallback
}

func localizedHelpText(labels []masterdata.AttributeLabel, locale masterdata.Locale) *string {
	var fallback *string
	for _, l := range labels {
		if l.Locale == locale {
			return l.HelpText
		}
		if l.Locale == masterdata.LocaleTR {
			fallback = l.HelpText
		}
	}
	return fallback
}

func localizedOptionLabel(labels []masterdata.OptionLabel, locale masterdata.Locale) string {
	var fallback string
	for _, l := range labels {
		if l.Locale == locale {
			return l.Label
		}
		if l.Locale == masterdata.LocaleTR {
			fallback = l.Label
		}
	}
	return fallback
}

func parentIDString(id *masterdata.ID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

func writeMasterDataError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, masterdata.ErrInvalidLocale), errors.Is(err, masterdata.ErrInvalidCategory),
		errors.Is(err, masterdata.ErrInvalidSchema), errors.Is(err, masterdata.ErrZeroID),
		errors.Is(err, masterdata.ErrInvalidCode):
		writeError(w, http.StatusBadRequest, "bad_request")
	case errors.Is(err, masterdata.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	default:
		writeError(w, http.StatusServiceUnavailable, "unavailable")
	}
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, errorResponse{Error: code})
}

func writePublicJSON(w http.ResponseWriter, body any) {
	w.Header().Set("Cache-Control", publicCacheControl)
	writeJSON(w, http.StatusOK, body)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
