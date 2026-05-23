package main

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"html"
	"log"
	"net/smtp"
	"strings"
	"time"
)

// sendDigest emails the AI analysis to the configured recipient. No-op when
// EMAIL_DIGEST_ENABLED is false or any required SMTP field is missing.
func (app *App) sendDigest(result *AnalysisResult) {
	c := app.config
	if !c.EmailDigestEnabled {
		return
	}
	missing := []string{}
	if c.SMTPHost == "" {
		missing = append(missing, "SMTP_HOST")
	}
	if c.SMTPPort == 0 {
		missing = append(missing, "SMTP_PORT")
	}
	if c.SMTPUsername == "" {
		missing = append(missing, "SMTP_USERNAME")
	}
	if c.SMTPPassword == "" {
		missing = append(missing, "SMTP_PASSWORD")
	}
	if c.EmailFrom == "" {
		missing = append(missing, "EMAIL_FROM")
	}
	if c.EmailTo == "" {
		missing = append(missing, "EMAIL_TO")
	}
	if len(missing) > 0 {
		log.Printf("email digest: skipping, missing env vars: %s", strings.Join(missing, ", "))
		return
	}

	subject := digestSubject(result)
	htmlBody := digestHTML(result, c.DashboardPublicURL)
	textBody := digestText(result, c.DashboardPublicURL)

	if err := sendMail(c.SMTPHost, c.SMTPPort, c.SMTPUsername, c.SMTPPassword,
		c.EmailFrom, c.EmailTo, subject, textBody, htmlBody); err != nil {
		log.Printf("email digest: send failed: %v", err)
		return
	}
	log.Printf("email digest: sent to %s", c.EmailTo)
}

func digestSubject(r *AnalysisResult) string {
	date := time.Now().Format("Jan 2")
	health := r.OverallHealth
	if health == "" {
		health = "report"
	}
	return fmt.Sprintf("Gardyn daily digest %s - %s", date, health)
}

// digestHTML produces an inline-styled HTML email body. We avoid <style>
// blocks because many clients (notably Gmail) strip them.
func digestHTML(r *AnalysisResult, dashboardURL string) string {
	var b strings.Builder
	b.WriteString(`<html><body style="font-family:-apple-system,BlinkMacSystemFont,Segoe UI,Helvetica,Arial,sans-serif;color:#222;line-height:1.45;max-width:680px;margin:auto;padding:16px;">`)

	b.WriteString(`<table style="width:100%;border-collapse:collapse;margin-bottom:14px;"><tr>`)
	b.WriteString(`<td style="font-size:20px;font-weight:700;">Gardyn daily digest</td>`)
	b.WriteString(`<td style="text-align:right;">` + healthBadge(r.OverallHealth) + `</td>`)
	b.WriteString(`</tr></table>`)

	if r.Error != "" {
		b.WriteString(`<div style="background:#fff8e1;border-left:3px solid #f9a825;padding:8px 12px;margin-bottom:12px;font-size:13px;color:#5d4037;">`)
		b.WriteString(html.EscapeString(r.Error))
		b.WriteString(`</div>`)
	}

	if r.ExecutiveSummary != "" {
		b.WriteString(`<p style="font-size:15px;color:#333;">`)
		b.WriteString(html.EscapeString(r.ExecutiveSummary))
		b.WriteString(`</p>`)
	}

	writeSection(&b, "Top recommendations", r.Recommendations, true)

	if len(r.PerPlanting) > 0 {
		b.WriteString(sectionTitle("Per-planting"))
		b.WriteString(`<table style="width:100%;border-collapse:collapse;font-size:13px;margin-bottom:14px;">`)
		for _, p := range r.PerPlanting {
			b.WriteString(`<tr style="border-bottom:1px solid #eee;">`)
			b.WriteString(`<td style="padding:6px 8px;font-family:ui-monospace,Menlo,monospace;font-weight:600;color:#2e7d32;width:42px;vertical-align:top;">`)
			b.WriteString(html.EscapeString(p.Pod))
			b.WriteString(`</td>`)
			b.WriteString(`<td style="padding:6px 8px;width:160px;vertical-align:top;">`)
			b.WriteString(html.EscapeString(p.Variety))
			b.WriteString(`<div style="color:#888;font-size:11px;text-transform:uppercase;letter-spacing:0.4px;margin-top:2px;">`)
			b.WriteString(html.EscapeString(p.GrowthStage))
			if p.ProjectedHarvestDate != "" {
				b.WriteString(" &middot; harvest " + html.EscapeString(p.ProjectedHarvestDate))
			}
			if p.YieldEstimateG > 0 {
				b.WriteString(fmt.Sprintf(" &middot; ~%.0fg", p.YieldEstimateG))
			}
			b.WriteString(`</div></td>`)
			b.WriteString(`<td style="padding:6px 8px;color:#444;vertical-align:top;">`)
			if len(p.ActionsToday) > 0 {
				b.WriteString(`<ul style="margin:0 0 0 16px;padding:0;">`)
				for _, a := range p.ActionsToday {
					b.WriteString(`<li>` + html.EscapeString(a) + `</li>`)
				}
				b.WriteString(`</ul>`)
			} else {
				b.WriteString(html.EscapeString(p.Assessment))
			}
			b.WriteString(`</td>`)
			b.WriteString(`</tr>`)
		}
		b.WriteString(`</table>`)
	}

	if len(r.InventoryDiscrepancies) > 0 {
		b.WriteString(sectionTitle("Inventory vs. photos"))
		for _, d := range r.InventoryDiscrepancies {
			b.WriteString(`<div style="background:#fff8e1;border-left:3px solid #f9a825;padding:8px 12px;margin-bottom:6px;font-size:13px;">`)
			b.WriteString(`<strong>` + html.EscapeString(d.Pod) + `</strong> &middot; `)
			b.WriteString(`<span style="color:#888;">` + html.EscapeString(strings.ReplaceAll(d.Type, "_", " ")) + `</span> &middot; `)
			b.WriteString(html.EscapeString(d.Description))
			b.WriteString(`</div>`)
		}
	}

	if len(r.SevenDayPlan) > 0 {
		b.WriteString(sectionTitle("7-day plan"))
		for _, d := range r.SevenDayPlan {
			b.WriteString(`<div style="background:#fafafa;border-left:3px solid #4caf50;padding:8px 12px;margin-bottom:6px;font-size:13px;">`)
			b.WriteString(`<strong style="text-transform:uppercase;letter-spacing:0.4px;color:#555;font-size:11px;">` + html.EscapeString(d.Day) + `</strong>`)
			b.WriteString(`<ul style="margin:4px 0 0 18px;padding:0;color:#444;">`)
			for _, a := range d.Actions {
				b.WriteString(`<li>` + html.EscapeString(a) + `</li>`)
			}
			b.WriteString(`</ul></div>`)
		}
	}

	if len(r.RiskForecast) > 0 {
		b.WriteString(sectionTitle("Risks"))
		for _, risk := range r.RiskForecast {
			bg := "#fafafa"
			border := "#9e9e9e"
			switch strings.ToLower(risk.Likelihood) {
			case "medium":
				bg = "#fff8e1"
				border = "#ffa726"
			case "high":
				bg = "#ffebee"
				border = "#e57373"
			}
			b.WriteString(fmt.Sprintf(`<div style="background:%s;border-left:3px solid %s;padding:8px 12px;margin-bottom:6px;font-size:13px;">`, bg, border))
			b.WriteString(`<strong>` + html.EscapeString(risk.Risk) + `</strong> `)
			b.WriteString(`<span style="text-transform:uppercase;letter-spacing:0.4px;color:#888;font-size:11px;">` + html.EscapeString(risk.Likelihood) + `</span>`)
			b.WriteString(`<div style="color:#444;margin-top:4px;">` + html.EscapeString(risk.Rationale) + `</div>`)
			b.WriteString(`<div style="color:#666;margin-top:2px;"><em>Mitigation:</em> ` + html.EscapeString(risk.Mitigation) + `</div>`)
			b.WriteString(`</div>`)
		}
	}

	if dashboardURL != "" {
		b.WriteString(`<p style="margin-top:18px;font-size:13px;color:#888;border-top:1px solid #eee;padding-top:10px;">`)
		b.WriteString(`<a href="` + html.EscapeString(dashboardURL) + `" style="color:#2e7d32;text-decoration:none;font-weight:600;">Open dashboard &rarr;</a>`)
		if r.Model != "" {
			b.WriteString(fmt.Sprintf(` &middot; %s &middot; %d in / %d out tokens`,
				html.EscapeString(r.Model), r.InputTokens, r.OutputTokens))
		}
		b.WriteString(`</p>`)
	}

	b.WriteString(`</body></html>`)
	return b.String()
}

func sectionTitle(t string) string {
	return `<h3 style="font-size:12px;text-transform:uppercase;letter-spacing:0.5px;color:#888;margin:14px 0 6px;">` + html.EscapeString(t) + `</h3>`
}

func writeSection(b *strings.Builder, title string, items []string, ordered bool) {
	if len(items) == 0 {
		return
	}
	b.WriteString(sectionTitle(title))
	tag := "ul"
	if ordered {
		tag = "ol"
	}
	b.WriteString(`<` + tag + ` style="margin:0 0 14px 18px;padding:0;font-size:14px;color:#333;">`)
	for _, i := range items {
		b.WriteString(`<li style="margin-bottom:4px;">` + html.EscapeString(i) + `</li>`)
	}
	b.WriteString(`</` + tag + `>`)
}

func healthBadge(h string) string {
	color := "#9e9e9e"
	bg := "#eee"
	switch strings.ToLower(h) {
	case "thriving":
		color, bg = "#1b5e20", "#c8e6c9"
	case "healthy":
		color, bg = "#2e7d32", "#e8f5e9"
	case "stressed":
		color, bg = "#ef6c00", "#fff8e1"
	case "concerning":
		color, bg = "#c62828", "#ffebee"
	}
	if h == "" {
		h = "unknown"
	}
	return fmt.Sprintf(`<span style="display:inline-block;font-size:12px;font-weight:700;text-transform:uppercase;letter-spacing:0.5px;padding:4px 10px;border-radius:4px;background:%s;color:%s;">%s</span>`,
		bg, color, html.EscapeString(h))
}

// digestText is the plain-text fallback alternative.
func digestText(r *AnalysisResult, dashboardURL string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "GARDYN DAILY DIGEST\nOverall: %s\n\n", strings.ToUpper(r.OverallHealth))
	if r.Error != "" {
		fmt.Fprintf(&b, "Warning: %s\n\n", r.Error)
	}
	if r.ExecutiveSummary != "" {
		fmt.Fprintln(&b, r.ExecutiveSummary)
		fmt.Fprintln(&b)
	}
	if len(r.Recommendations) > 0 {
		fmt.Fprintln(&b, "TOP RECOMMENDATIONS")
		for i, rec := range r.Recommendations {
			fmt.Fprintf(&b, "%d. %s\n", i+1, rec)
		}
		fmt.Fprintln(&b)
	}
	if len(r.PerPlanting) > 0 {
		fmt.Fprintln(&b, "PER-PLANTING")
		for _, p := range r.PerPlanting {
			fmt.Fprintf(&b, "- %s %s (%s)\n", p.Pod, p.Variety, p.GrowthStage)
			for _, a := range p.ActionsToday {
				fmt.Fprintf(&b, "    * %s\n", a)
			}
		}
		fmt.Fprintln(&b)
	}
	if len(r.SevenDayPlan) > 0 {
		fmt.Fprintln(&b, "7-DAY PLAN")
		for _, d := range r.SevenDayPlan {
			fmt.Fprintf(&b, "- %s:\n", d.Day)
			for _, a := range d.Actions {
				fmt.Fprintf(&b, "    * %s\n", a)
			}
		}
		fmt.Fprintln(&b)
	}
	if dashboardURL != "" {
		fmt.Fprintf(&b, "Dashboard: %s\n", dashboardURL)
	}
	return b.String()
}

// sendMail dispatches a multipart/alternative email. Supports STARTTLS (587)
// and implicit TLS (465).
func sendMail(host string, port int, username, password, from, to, subject, text, htmlBody string) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", username, password, host)

	boundary := fmt.Sprintf("gardyn-%d", time.Now().UnixNano())
	headers := []string{
		"From: " + from,
		"To: " + to,
		"Subject: " + mimeEncode(subject),
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=" + boundary,
	}
	body := strings.Join(headers, "\r\n") + "\r\n\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		text + "\r\n" +
		"--" + boundary + "\r\n" +
		"Content-Type: text/html; charset=UTF-8\r\n\r\n" +
		htmlBody + "\r\n" +
		"--" + boundary + "--\r\n"

	if port == 465 {
		return sendImplicitTLS(addr, host, auth, from, to, []byte(body))
	}
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(body))
}

func sendImplicitTLS(addr, host string, auth smtp.Auth, from, to string, body []byte) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("smtp new client: %w", err)
	}
	defer client.Quit()
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("RCPT TO: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		w.Close()
		return fmt.Errorf("write body: %w", err)
	}
	return w.Close()
}

// mimeEncode wraps non-ASCII subjects in RFC 2047 encoded-word format.
func mimeEncode(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(s)))
		}
	}
	return s
}
