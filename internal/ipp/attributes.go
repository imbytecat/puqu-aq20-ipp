package ipp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/OpenPrinting/goipp"

	"github.com/imbytecat/puqu-ipp-bridge/internal/raster"
	"github.com/imbytecat/puqu-ipp-bridge/internal/store"
)

func (s *Server) getPrinterAttributes(ctx context.Context, request *goipp.Message, host string) *goipp.Message {
	target, err := s.store.Printer(ctx, s.printerID)
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	profile, err := s.store.Profile(ctx, target.ProfileID)
	if err != nil {
		return response(request, goipp.StatusErrorNotAcceptingJobs, "label profile is unavailable")
	}
	jobs, err := s.store.JobsByPrinter(ctx, s.printerID, 500)
	if err != nil {
		return response(request, goipp.StatusErrorInternal, err.Error())
	}
	queued := 0
	for _, job := range jobs {
		if job.State == "pending" || job.State == "processing" {
			queued++
		}
	}
	state := s.printer.Status()
	printerState := int32(3)
	reasons := "none"
	if state.Busy {
		printerState = 4
	} else if !state.Connected || target.Enabled != 1 {
		printerState = 5
		reasons = "offline"
	}

	uri := printerURI(host, s.slug)
	media := mediaName(profile)
	formats := []goipp.Value{goipp.String(raster.FormatPWG), goipp.String(raster.FormatJPEG)}
	operations := make([]goipp.Value, len(supportedOperations))
	for i, operation := range supportedOperations {
		operations[i] = goipp.Integer(operation)
	}
	mediaSize := goipp.Collection{
		goipp.MakeAttr("x-dimension", goipp.TagInteger, goipp.Integer(profile.WidthUm/10)),
		goipp.MakeAttr("y-dimension", goipp.TagInteger, goipp.Integer(profile.HeightUm/10)),
	}
	mediaCol := goipp.Collection{
		goipp.MakeAttr("media-key", goipp.TagKeyword, goipp.String(media)),
		goipp.MakeAttr("media-size-name", goipp.TagKeyword, goipp.String(media)),
		goipp.MakeAttr("media-size", goipp.TagBeginCollection, mediaSize),
		goipp.MakeAttr("media-source", goipp.TagKeyword, goipp.String("main")),
		goipp.MakeAttr("media-type", goipp.TagKeyword, goipp.String("labels")),
		goipp.MakeAttr("media-left-margin", goipp.TagInteger, goipp.Integer(0)),
		goipp.MakeAttr("media-right-margin", goipp.TagInteger, goipp.Integer(0)),
		goipp.MakeAttr("media-top-margin", goipp.TagInteger, goipp.Integer(0)),
		goipp.MakeAttr("media-bottom-margin", goipp.TagInteger, goipp.Integer(0)),
	}

	reply := response(request, goipp.StatusOk, "")
	attrs := &reply.Printer
	attrs.Add(goipp.MakeAttr("printer-uri-supported", goipp.TagURI, goipp.String(uri)))
	attrs.Add(goipp.MakeAttr("uri-authentication-supported", goipp.TagKeyword, goipp.String("none")))
	attrs.Add(goipp.MakeAttr("uri-security-supported", goipp.TagKeyword, goipp.String("none")))
	attrs.Add(goipp.MakeAttr("printer-name", goipp.TagName, goipp.String(target.Name)))
	attrs.Add(goipp.MakeAttr("printer-info", goipp.TagText, goipp.String("PUQU AQ20 Bluetooth label printer")))
	attrs.Add(goipp.MakeAttr("printer-location", goipp.TagText, goipp.String("Local Bluetooth bridge")))
	attrs.Add(goipp.MakeAttr("printer-make-and-model", goipp.TagText, goipp.String("PUQU IPP Bridge")))
	attrs.Add(goipp.MakeAttr("printer-more-info", goipp.TagURI, goipp.String(httpURI(host, "/"))))
	attrs.Add(goipp.MakeAttr("printer-uuid", goipp.TagURI, goipp.String("urn:uuid:"+target.Uuid)))
	attrs.Add(goipp.MakeAttr("printer-device-id", goipp.TagText, goipp.String("MFG:PUQU;MDL:AQ20;CMD:PWGRaster,JPEG;")))
	attrs.Add(goipp.MakeAttr("printer-state", goipp.TagEnum, goipp.Integer(printerState)))
	attrs.Add(goipp.MakeAttr("printer-state-reasons", goipp.TagKeyword, goipp.String(reasons)))
	attrs.Add(goipp.MakeAttr("printer-state-message", goipp.TagText, goipp.String(state.LastError)))
	attrs.Add(goipp.MakeAttr("printer-is-accepting-jobs", goipp.TagBoolean, goipp.Boolean(target.Enabled == 1 && len(s.queue) < cap(s.queue))))
	attrs.Add(goipp.MakeAttr("queued-job-count", goipp.TagInteger, goipp.Integer(queued)))
	attrs.Add(goipp.MakeAttr("printer-up-time", goipp.TagInteger, goipp.Integer(time.Since(s.started)/time.Second)))
	attrs.Add(goipp.MakeAttr("printer-current-time", goipp.TagDateTime, goipp.Time{Time: time.Now()}))
	attrs.Add(goipp.MakeAttr("printer-config-change-date-time", goipp.TagDateTime, goipp.Time{Time: time.UnixMilli(target.UpdatedAt)}))
	attrs.Add(goipp.MakeAttr("printer-state-change-date-time", goipp.TagDateTime, goipp.Time{Time: s.started}))
	attrs.Add(goipp.MakeAttr("printer-state-change-time", goipp.TagInteger, goipp.Integer(0)))
	attrs.Add(goipp.MakeAttr("printer-config-change-time", goipp.TagInteger, goipp.Integer(max(0, target.UpdatedAt/1000-s.started.Unix()))))
	attrs.Add(goipp.MakeAttr("ipp-versions-supported", goipp.TagKeyword, goipp.String("1.1"), goipp.String("2.0")))
	attrs.Add(goipp.MakeAttr("operations-supported", goipp.TagEnum, operations[0], operations[1:]...))
	attrs.Add(goipp.MakeAttr("identify-actions-default", goipp.TagKeyword, goipp.String("display")))
	attrs.Add(goipp.MakeAttr("identify-actions-supported", goipp.TagKeyword, goipp.String("display")))
	attrs.Add(goipp.MakeAttr("charset-configured", goipp.TagCharset, goipp.String("utf-8")))
	attrs.Add(goipp.MakeAttr("charset-supported", goipp.TagCharset, goipp.String("utf-8")))
	attrs.Add(goipp.MakeAttr("natural-language-configured", goipp.TagLanguage, goipp.String("en-us")))
	attrs.Add(goipp.MakeAttr("generated-natural-language-supported", goipp.TagLanguage, goipp.String("en-us")))
	attrs.Add(goipp.MakeAttr("document-format-default", goipp.TagMimeType, goipp.String(raster.FormatPWG)))
	attrs.Add(goipp.MakeAttr("document-format-supported", goipp.TagMimeType, formats[0], formats[1:]...))
	attrs.Add(goipp.MakeAttr("compression-supported", goipp.TagKeyword, goipp.String("none")))
	attrs.Add(goipp.MakeAttr("color-supported", goipp.TagBoolean, goipp.Boolean(false)))
	attrs.Add(goipp.MakeAttr("copies-default", goipp.TagInteger, goipp.Integer(1)))
	attrs.Add(goipp.MakeAttr("copies-supported", goipp.TagRange, goipp.Range{Lower: 1, Upper: 999}))
	attrs.Add(goipp.MakeAttr("finishings-default", goipp.TagEnum, goipp.Integer(3)))
	attrs.Add(goipp.MakeAttr("finishings-supported", goipp.TagEnum, goipp.Integer(3)))
	attrs.Add(goipp.MakeAttr("output-bin-default", goipp.TagKeyword, goipp.String("face-down")))
	attrs.Add(goipp.MakeAttr("output-bin-supported", goipp.TagKeyword, goipp.String("face-down")))
	attrs.Add(goipp.MakeAttr("pages-per-minute", goipp.TagInteger, goipp.Integer(10)))
	attrs.Add(goipp.MakeAttr("multiple-document-jobs-supported", goipp.TagBoolean, goipp.Boolean(false)))
	attrs.Add(goipp.MakeAttr("multiple-operation-time-out", goipp.TagInteger, goipp.Integer(60)))
	attrs.Add(goipp.MakeAttr("multiple-operation-time-out-action", goipp.TagKeyword, goipp.String("abort-job")))
	attrs.Add(goipp.MakeAttr("multiple-document-handling-default", goipp.TagKeyword, goipp.String("separate-documents-uncollated-copies")))
	attrs.Add(goipp.MakeAttr("multiple-document-handling-supported", goipp.TagKeyword, goipp.String("separate-documents-uncollated-copies")))
	attrs.Add(goipp.MakeAttr("page-ranges-supported", goipp.TagBoolean, goipp.Boolean(false)))
	attrs.Add(goipp.MakeAttr("pdl-override-supported", goipp.TagKeyword, goipp.String("not-attempted")))
	attrs.Add(goipp.MakeAttr("job-ids-supported", goipp.TagBoolean, goipp.Boolean(true)))
	attrs.Add(goipp.MakeAttr("preferred-attributes-supported", goipp.TagBoolean, goipp.Boolean(false)))
	attrs.Add(goipp.MakeAttr("overrides-supported", goipp.TagKeyword, goipp.String("document-number"), goipp.String("pages")))
	attrs.Add(goipp.MakeAttr("job-creation-attributes-supported", goipp.TagKeyword,
		goipp.String("copies"), goipp.String("document-format"), goipp.String("job-name"),
		goipp.String("media"), goipp.String("media-col"), goipp.String("print-color-mode"),
		goipp.String("printer-resolution"), goipp.String("sides")))
	attrs.Add(goipp.MakeAttr("which-jobs-supported", goipp.TagKeyword,
		goipp.String("all"), goipp.String("completed"), goipp.String("not-completed"),
		goipp.String("aborted"), goipp.String("canceled"), goipp.String("pending"), goipp.String("processing")))
	attrs.Add(goipp.MakeAttr("media-default", goipp.TagKeyword, goipp.String(media)))
	attrs.Add(goipp.MakeAttr("media-ready", goipp.TagKeyword, goipp.String(media)))
	attrs.Add(goipp.MakeAttr("media-supported", goipp.TagKeyword, goipp.String(media)))
	attrs.Add(goipp.MakeAttr("media-col-default", goipp.TagBeginCollection, mediaCol))
	attrs.Add(goipp.MakeAttr("media-col-ready", goipp.TagBeginCollection, mediaCol))
	attrs.Add(goipp.MakeAttr("media-col-database", goipp.TagBeginCollection, mediaCol))
	attrs.Add(goipp.MakeAttr("media-col-supported", goipp.TagKeyword,
		goipp.String("media-size"), goipp.String("media-size-name"), goipp.String("media-source"),
		goipp.String("media-type"), goipp.String("media-left-margin"), goipp.String("media-right-margin"),
		goipp.String("media-top-margin"), goipp.String("media-bottom-margin")))
	attrs.Add(goipp.MakeAttr("media-size-supported", goipp.TagBeginCollection, mediaSize))
	attrs.Add(goipp.MakeAttr("media-source-supported", goipp.TagKeyword, goipp.String("main")))
	attrs.Add(goipp.MakeAttr("media-type-supported", goipp.TagKeyword, goipp.String("labels")))
	attrs.Add(goipp.MakeAttr("media-left-margin-supported", goipp.TagInteger, goipp.Integer(0)))
	attrs.Add(goipp.MakeAttr("media-right-margin-supported", goipp.TagInteger, goipp.Integer(0)))
	attrs.Add(goipp.MakeAttr("media-top-margin-supported", goipp.TagInteger, goipp.Integer(0)))
	attrs.Add(goipp.MakeAttr("media-bottom-margin-supported", goipp.TagInteger, goipp.Integer(0)))
	attrs.Add(goipp.MakeAttr("printer-resolution-default", goipp.TagResolution, goipp.Resolution{Xres: 203, Yres: 203, Units: goipp.UnitsDpi}))
	attrs.Add(goipp.MakeAttr("printer-resolution-supported", goipp.TagResolution, goipp.Resolution{Xres: 203, Yres: 203, Units: goipp.UnitsDpi}))
	attrs.Add(goipp.MakeAttr("print-color-mode-default", goipp.TagKeyword, goipp.String("monochrome")))
	attrs.Add(goipp.MakeAttr("print-color-mode-supported", goipp.TagKeyword, goipp.String("monochrome")))
	attrs.Add(goipp.MakeAttr("sides-default", goipp.TagKeyword, goipp.String("one-sided")))
	attrs.Add(goipp.MakeAttr("sides-supported", goipp.TagKeyword, goipp.String("one-sided")))
	attrs.Add(goipp.MakeAttr("orientation-requested-default", goipp.TagEnum, goipp.Integer(3)))
	attrs.Add(goipp.MakeAttr("orientation-requested-supported", goipp.TagEnum, goipp.Integer(3)))
	attrs.Add(goipp.MakeAttr("print-quality-default", goipp.TagEnum, goipp.Integer(4)))
	attrs.Add(goipp.MakeAttr("print-quality-supported", goipp.TagEnum, goipp.Integer(4)))
	attrs.Add(goipp.MakeAttr("print-content-optimize-default", goipp.TagKeyword, goipp.String("auto")))
	attrs.Add(goipp.MakeAttr("print-content-optimize-supported", goipp.TagKeyword,
		goipp.String("auto"), goipp.String("graphic"), goipp.String("photo"), goipp.String("text"), goipp.String("text-and-graphic")))
	attrs.Add(goipp.MakeAttr("print-rendering-intent-default", goipp.TagKeyword, goipp.String("auto")))
	attrs.Add(goipp.MakeAttr("print-rendering-intent-supported", goipp.TagKeyword,
		goipp.String("auto"), goipp.String("absolute"), goipp.String("perceptual"), goipp.String("relative"), goipp.String("relative-bpc"), goipp.String("saturation")))
	attrs.Add(goipp.MakeAttr("pwg-raster-document-resolution-supported", goipp.TagResolution, goipp.Resolution{Xres: 203, Yres: 203, Units: goipp.UnitsDpi}))
	attrs.Add(goipp.MakeAttr("pwg-raster-document-type-supported", goipp.TagKeyword,
		goipp.String("black_1"), goipp.String("black_8"), goipp.String("sgray_1"), goipp.String("sgray_8"), goipp.String("srgb_8")))
	attrs.Add(goipp.MakeAttr("pwg-raster-document-sheet-back", goipp.TagKeyword, goipp.String("normal")))
	attrs.Add(goipp.MakeAttr("printer-geo-location", goipp.TagUnknown, goipp.Void{}))
	attrs.Add(goipp.MakeAttr("printer-get-attributes-supported", goipp.TagKeyword, goipp.String("document-format")))
	attrs.Add(goipp.MakeAttr("printer-icons", goipp.TagURI, goipp.String(httpURI(host, "/icon.svg"))))
	attrs.Add(goipp.MakeAttr("printer-organization", goipp.TagText, goipp.String("PUQU IPP Bridge")))
	attrs.Add(goipp.MakeAttr("printer-organizational-unit", goipp.TagText, goipp.String("Local printers")))
	attrs.Add(goipp.MakeAttr("printer-supply", goipp.TagString, goipp.Binary("index=1;class=supplyThatIsFilled;type=unknown;unit=percent;maxcapacity=100;level=-2;")))
	attrs.Add(goipp.MakeAttr("printer-supply-description", goipp.TagText, goipp.String("Thermal label media")))
	attrs.Add(goipp.MakeAttr("printer-supply-info-uri", goipp.TagURI, goipp.String(httpURI(host, "/"))))
	reply.Printer = filterPrinterAttributes(request, reply.Printer)
	return reply
}
func filterPrinterAttributes(request *goipp.Message, attrs goipp.Attributes) goipp.Attributes {
	requested := stringAttrs(request, "requested-attributes")
	if len(requested) == 0 {
		requested = []string{"all"}
	}
	exact := make(map[string]bool, len(requested))
	all, description, template := false, false, false
	for _, name := range requested {
		switch name {
		case "none":
			return nil
		case "all":
			all = true
		case "printer-description":
			description = true
		case "job-template":
			template = true
		default:
			exact[name] = true
		}
	}
	filtered := make(goipp.Attributes, 0, len(attrs))
	for _, attribute := range attrs {
		jobTemplate := isJobTemplateAttribute(attribute.Name)
		if all || exact[attribute.Name] || description && !jobTemplate || template && jobTemplate {
			filtered = append(filtered, attribute)
		}
	}
	return filtered
}

func isJobTemplateAttribute(name string) bool {
	switch {
	case strings.HasPrefix(name, "copies-"),
		strings.HasPrefix(name, "finishings-"),
		strings.HasPrefix(name, "media-"),
		strings.HasPrefix(name, "multiple-document-handling-"),
		strings.HasPrefix(name, "orientation-requested-"),
		strings.HasPrefix(name, "output-bin-"),
		strings.HasPrefix(name, "print-color-mode-"),
		strings.HasPrefix(name, "print-content-optimize-"),
		strings.HasPrefix(name, "print-quality-"),
		strings.HasPrefix(name, "print-rendering-intent-"),
		strings.HasPrefix(name, "printer-resolution-"),
		strings.HasPrefix(name, "sides-"):
		return true
	case name == "overrides-supported", name == "page-ranges-supported":
		return true
	default:
		return false
	}
}

func fmtProfile(profile *store.Profile) string {
	return fmt.Sprintf("%g × %g mm", float64(profile.WidthUm)/1000, float64(profile.HeightUm)/1000)
}
