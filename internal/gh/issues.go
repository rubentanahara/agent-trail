package gh

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

const (
	IssueLimit     = 100
	titleFieldName = "Title"
	issueContent   = "Issue"
	StatusField    = "Status"
)

const fieldValueFragment = `__typename
... on ProjectV2ItemFieldSingleSelectValue{name field{... on ProjectV2FieldCommon{name}}}
... on ProjectV2ItemFieldTextValue{text field{... on ProjectV2FieldCommon{name}}}
... on ProjectV2ItemFieldNumberValue{number field{... on ProjectV2FieldCommon{name}}}
... on ProjectV2ItemFieldDateValue{date field{... on ProjectV2FieldCommon{name}}}
... on ProjectV2ItemFieldIterationValue{title field{... on ProjectV2FieldCommon{name}}}`

const issueFields = `number title state url createdAt updatedAt author{login} assignees(first:5){nodes{login}} labels(first:10){nodes{name}} milestone{title} comments{totalCount} parent{number title} subIssuesSummary{total completed}`

const boardQuery = `query($owner:String!,$name:String!){repository(owner:$owner,name:$name){` +
	`projectsV2(first:10){nodes{number title url field(name:"Status"){... on ProjectV2SingleSelectField{options{name}}} items(first:100){totalCount nodes{fieldValues(first:20){nodes{` + fieldValueFragment + `}} content{__typename ... on Issue{` + issueFields + `}}}}}}` +
	`issues(first:100,states:OPEN,orderBy:{field:UPDATED_AT,direction:DESC}){totalCount nodes{` + issueFields + `}}}}`

const issueDetailQuery = `query($owner:String!,$name:String!,$number:Int!){repository(owner:$owner,name:$name){issue(number:$number){body ` +
	`comments(first:50){totalCount nodes{id author{login} body createdAt url}} ` +
	`closedByPullRequestsReferences(first:10){nodes{number title state url}} ` +
	`subIssues(first:50){nodes{number title state url}}}}}`

type Field struct {
	Name  string
	Value string
}

type Issue struct {
	Number       int
	Title        string
	State        string
	URL          string
	Author       string
	Assignees    []string
	Labels       []string
	Milestone    string
	Comments     int
	ParentNumber int
	ParentTitle  string
	SubTotal     int
	SubDone      int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Fields       []Field
}

func (i Issue) Field(name string) string {
	for _, field := range i.Fields {
		if field.Name == name {
			return field.Value
		}
	}
	return ""
}

type Project struct {
	Number  int
	Title   string
	URL     string
	Total   int
	Columns []string
	Issues  []Issue
}

type Board struct {
	Projects  []Project
	Open      []Issue
	OpenTotal int
}

type IssueRef struct {
	Number int
	Title  string
	State  string
	URL    string
}

type IssueComment struct {
	ID        string
	Author    string
	Body      string
	URL       string
	CreatedAt time.Time
}

type IssueDetail struct {
	Body         string
	Comments     []IssueComment
	CommentTotal int
	ClosedBy     []IssueRef
	SubIssues    []IssueRef
}

type rawIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Author    *struct {
		Login string `json:"login"`
	} `json:"author"`
	Assignees struct {
		Nodes []struct {
			Login string `json:"login"`
		} `json:"nodes"`
	} `json:"assignees"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Milestone *struct {
		Title string `json:"title"`
	} `json:"milestone"`
	Comments struct {
		TotalCount int `json:"totalCount"`
	} `json:"comments"`
	Parent *struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
	} `json:"parent"`
	SubSummary *struct {
		Total     int `json:"total"`
		Completed int `json:"completed"`
	} `json:"subIssuesSummary"`
}

type rawFieldValue struct {
	Name   string   `json:"name"`
	Text   string   `json:"text"`
	Number *float64 `json:"number"`
	Date   string   `json:"date"`
	Title  string   `json:"title"`
	Field  struct {
		Name string `json:"name"`
	} `json:"field"`
}

type rawProjectItem struct {
	FieldValues struct {
		Nodes []rawFieldValue `json:"nodes"`
	} `json:"fieldValues"`
	Content struct {
		Kind string `json:"__typename"`
		rawIssue
	} `json:"content"`
}

type rawBoard struct {
	Data struct {
		Repository struct {
			Projects struct {
				Nodes []struct {
					Number int    `json:"number"`
					Title  string `json:"title"`
					URL    string `json:"url"`
					Status struct {
						Options []struct {
							Name string `json:"name"`
						} `json:"options"`
					} `json:"field"`
					Items struct {
						TotalCount int              `json:"totalCount"`
						Nodes      []rawProjectItem `json:"nodes"`
					} `json:"items"`
				} `json:"nodes"`
			} `json:"projectsV2"`
			Issues struct {
				TotalCount int        `json:"totalCount"`
				Nodes      []rawIssue `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	} `json:"data"`
}

type rawRef struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"url"`
}

type rawIssueDetail struct {
	Data struct {
		Repository struct {
			Issue struct {
				Body     string `json:"body"`
				Comments struct {
					TotalCount int `json:"totalCount"`
					Nodes      []struct {
						ID        string     `json:"id"`
						Author    *rawAuthor `json:"author"`
						Body      string     `json:"body"`
						URL       string     `json:"url"`
						CreatedAt time.Time  `json:"createdAt"`
					} `json:"nodes"`
				} `json:"comments"`
				ClosedBy struct {
					Nodes []rawRef `json:"nodes"`
				} `json:"closedByPullRequestsReferences"`
				SubIssues struct {
					Nodes []rawRef `json:"nodes"`
				} `json:"subIssues"`
			} `json:"issue"`
		} `json:"repository"`
	} `json:"data"`
}

func BoardArgs(owner, name string) []string {
	return []string{"api", "graphql", "-f", "query=" + boardQuery, "-f", "owner=" + owner, "-f", "name=" + name}
}

func IssueDetailArgs(owner, name string, number int) []string {
	return []string{"api", "graphql", "-f", "query=" + issueDetailQuery, "-f", "owner=" + owner, "-f", "name=" + name, "-F", "number=" + strconv.Itoa(number)}
}

func LoadBoard(ctx context.Context, run Runner, owner, name string) (Board, error) {
	out, err := run(ctx, BoardArgs(owner, name))
	if err != nil {
		return Board{}, err
	}
	return DecodeBoard(out)
}

func LoadIssueDetail(ctx context.Context, run Runner, owner, name string, number int) (IssueDetail, error) {
	out, err := run(ctx, IssueDetailArgs(owner, name, number))
	if err != nil {
		return IssueDetail{}, err
	}
	return DecodeIssueDetail(out)
}

func DecodeBoard(data []byte) (Board, error) {
	var raw rawBoard
	if err := json.Unmarshal(data, &raw); err != nil {
		return Board{}, err
	}
	repo := raw.Data.Repository
	board := Board{OpenTotal: repo.Issues.TotalCount}
	for _, issue := range repo.Issues.Nodes {
		board.Open = append(board.Open, issue.decode())
	}
	for _, node := range repo.Projects.Nodes {
		project := Project{Number: node.Number, Title: node.Title, URL: node.URL, Total: node.Items.TotalCount}
		for _, option := range node.Status.Options {
			project.Columns = append(project.Columns, option.Name)
		}
		for _, item := range node.Items.Nodes {
			if item.Content.Kind == issueContent {
				issue := item.Content.decode()
				issue.Fields = decodeFields(item.FieldValues.Nodes)
				project.Issues = append(project.Issues, issue)
			}
		}
		board.Projects = append(board.Projects, project)
	}
	return board, nil
}

func (r rawIssue) decode() Issue {
	issue := Issue{
		Number: r.Number, Title: r.Title, State: r.State, URL: r.URL,
		Comments: r.Comments.TotalCount, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
	if r.Author != nil {
		issue.Author = r.Author.Login
	}
	for _, assignee := range r.Assignees.Nodes {
		issue.Assignees = append(issue.Assignees, assignee.Login)
	}
	for _, label := range r.Labels.Nodes {
		issue.Labels = append(issue.Labels, label.Name)
	}
	if r.Milestone != nil {
		issue.Milestone = r.Milestone.Title
	}
	if r.Parent != nil {
		issue.ParentNumber, issue.ParentTitle = r.Parent.Number, r.Parent.Title
	}
	if r.SubSummary != nil {
		issue.SubTotal, issue.SubDone = r.SubSummary.Total, r.SubSummary.Completed
	}
	return issue
}

func decodeFields(values []rawFieldValue) []Field {
	var fields []Field
	for _, value := range values {
		if text := value.display(); value.Field.Name != "" && value.Field.Name != titleFieldName && text != "" {
			fields = append(fields, Field{Name: value.Field.Name, Value: text})
		}
	}
	return fields
}

func (v rawFieldValue) display() string {
	switch {
	case v.Name != "":
		return v.Name
	case v.Title != "":
		return v.Title
	case v.Date != "":
		return v.Date
	case v.Number != nil:
		return strconv.FormatFloat(*v.Number, 'f', -1, 64)
	}
	return v.Text
}

func DecodeIssueDetail(data []byte) (IssueDetail, error) {
	var raw rawIssueDetail
	if err := json.Unmarshal(data, &raw); err != nil {
		return IssueDetail{}, err
	}
	issue := raw.Data.Repository.Issue
	detail := IssueDetail{Body: issue.Body, CommentTotal: issue.Comments.TotalCount}
	for _, comment := range issue.Comments.Nodes {
		author := comment.Author.decode()
		detail.Comments = append(detail.Comments, IssueComment{ID: comment.ID, Author: author.Login, Body: comment.Body, URL: comment.URL, CreatedAt: comment.CreatedAt})
	}
	for _, ref := range issue.ClosedBy.Nodes {
		detail.ClosedBy = append(detail.ClosedBy, IssueRef(ref))
	}
	for _, ref := range issue.SubIssues.Nodes {
		detail.SubIssues = append(detail.SubIssues, IssueRef(ref))
	}
	return detail, nil
}
