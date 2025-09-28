package main

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"regexp"

	"github.com/PuerkitoBio/goquery"
	"github.com/gin-gonic/gin"
)

type Subject struct {
	Name    string `json:"ten_mon"`
	Group   string `json:"nhom"`
	Class   string `json:"lop"`
	Period  string `json:"tiet"`
	Room    string `json:"phong"`
	Teacher string `json:"gv"`
	Lessons string `json:"da_hoc"`
}

type DaySchedule struct {
	Sang  []Subject `json:"sang"`
	Chieu []Subject `json:"chieu"`
	Toi   []Subject `json:"toi"`
}

type Schedule struct {
	Class string                 `json:"class"`
	Week  string                 `json:"week"`
	Days  map[string]DaySchedule `json:"days"`
}

func parseHeader(input string) (week, className string) {
	re := regexp.MustCompile(`Tuần\s+(\d+).*lớp:\s*([A-Z0-9]+)`)
	matches := re.FindStringSubmatch(input)
	if len(matches) == 3 {
		week = matches[1]
		className = matches[2]
	}
	return
}

func splitSubjects(input string) []string {
	input = strings.ReplaceAll(input, " tiết ", " tiết\n")
	lines := strings.Split(input, "\n")
	var result []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result
}

func parseSubjects(input string) []Subject {
	if strings.Contains(input, "Nghỉ") {
		return nil
	}

	var subjects []Subject
	lines := splitSubjects(input)

	re := regexp.MustCompile(`^(.*?)(?:\((\d{2}[A-Z0-9]+)\))?- Nhóm: (\d+)- Lớp: ([A-Z0-9]+)(?: - nhom \d+)?- Tiết: ([0-9\-]+)- Phòng: ([A-Za-z0-9\.]+)- GV: ([^\-]+)- Đã học: (\d+/\d+)`)
	for _, line := range lines {
		m := re.FindStringSubmatch(line)
		if len(m) == 9 {
			subjects = append(subjects, Subject{
				Name:    strings.TrimSpace(m[1]),
				Group:   m[3],
				Class:   m[4],
				Period:  m[5],
				Room:    m[6],
				Teacher: strings.TrimSpace(m[7]),
				Lessons: m[8],
			})
		}
	}

	return subjects
}

func parseDay(dayLines []string) DaySchedule {
	day := DaySchedule{}
	for _, line := range dayLines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Sáng:") {
			day.Sang = parseSubjects(strings.TrimPrefix(line, "Sáng:"))
		} else if strings.HasPrefix(line, "Chiều:") {
			day.Chieu = parseSubjects(strings.TrimPrefix(line, "Chiều:"))
		} else if strings.HasPrefix(line, "Tối:") {
			day.Toi = parseSubjects(strings.TrimPrefix(line, "Tối:"))
		}
	}
	return day
}

func parseSchedule(input string) Schedule {
	week, className := parseHeader(input)
	lines := strings.Split(input, "\n")

	days := make(map[string]DaySchedule)
	var currentDay string
	var dayLines []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Thứ") || strings.HasPrefix(line, "Chủ nhật") {
			if currentDay != "" {
				days[currentDay] = parseDay(dayLines)
			}
			currentDay = strings.TrimSuffix(line, ":")
			dayLines = []string{}
		} else {
			dayLines = append(dayLines, line)
		}
	}
	if currentDay != "" {
		days[currentDay] = parseDay(dayLines)
	}

	return Schedule{
		Class: className,
		Week:  week,
		Days:  days,
	}
}

func main() {
	r := gin.Default()

	r.GET("/dlu", func(c *gin.Context) {
		year := c.Query("YearStudy")
		term := c.Query("TermID")
		week := c.Query("Week")
		classID := c.Query("ClassStudentID")

		if year == "" || term == "" || week == "" || classID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Missing query parameters"})
			return
		}

		url := fmt.Sprintf(
			"https://qlgd.dlu.edu.vn/public/DrawingClassStudentSchedules_Mau2?YearStudy=%s&TermID=%s&Week=%s&ClassStudentID=%s",
			year, term, week, classID,
		)

		tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		client := &http.Client{Transport: tr}

		resp, err := client.Get(url)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(body)))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var sb strings.Builder
		header := doc.Find("div > div[style]").First().Text()
		sb.WriteString(strings.TrimSpace(header) + "\n\n")

		doc.Find("table tr").Each(func(i int, s *goquery.Selection) {
			if i == 0 { return }
			day := strings.TrimSpace(s.Find("th").Text())
			if day == "" { return }
			sb.WriteString(day + ":\n")
			s.Find("td").Each(func(j int, td *goquery.Selection) {
				slot := map[int]string{0:"Sáng",1:"Chiều",2:"Tối"}[j]
				content := strings.TrimSpace(td.Text())
				if content == "" {
					sb.WriteString("  "+slot+": Nghỉ\n")
				} else {
					sb.WriteString("  "+slot+": "+strings.Join(strings.Fields(content)," ")+"\n")
				}
			})
			sb.WriteString("\n")
		})

		timetable := sb.String()
		schedule := parseSchedule(timetable)
		c.JSON(http.StatusOK, schedule)
	})

	log.Println("Server running at http://localhost:8080")
	r.Run(":8080")
}
