package powersched

import (
	"testing"
	"time"

	"github.com/go-co-op/gocron/v2"
)

func validSchedule() Schedule {
	return Schedule{
		ID:         "abc1234",
		Name:       "Morning wake",
		Enabled:    true,
		Method:     MethodWOL,
		Action:     ActionOn,
		Weekdays:   []int{1, 2, 3, 4, 5},
		Hour:       8,
		Minute:     30,
		Timezone:   "Europe/Berlin",
		MacAddress: "00:b0:d0:63:c2:26",
	}
}

func TestCronTab(t *testing.T) {
	tests := []struct {
		name string
		mut  func(*Schedule)
		want string
	}{
		{
			name: "weekdays with timezone",
			mut:  func(_ *Schedule) {},
			want: "TZ=Europe/Berlin 0 30 8 * * 1,2,3,4,5",
		},
		{
			name: "UTC omits the TZ prefix",
			mut:  func(s *Schedule) { s.Timezone = "UTC" },
			want: "0 30 8 * * 1,2,3,4,5",
		},
		{
			name: "empty timezone omits the TZ prefix",
			mut:  func(s *Schedule) { s.Timezone = "" },
			want: "0 30 8 * * 1,2,3,4,5",
		},
		{
			name: "sunday only",
			mut: func(s *Schedule) {
				s.Weekdays = []int{0}
				s.Hour = 0
				s.Minute = 0
				s.Timezone = "UTC"
			},
			want: "0 0 0 * * 0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSchedule()
			tt.mut(&s)
			if got := s.CronTab(); got != tt.want {
				t.Errorf("CronTab() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestCronTabIsAcceptedByGocron guards the format contract with the scheduler
// library: a crontab we generate but gocron rejects would silently disable a
// schedule at runtime.
func TestCronTabIsAcceptedByGocron(t *testing.T) {
	s, err := gocron.NewScheduler()
	if err != nil {
		t.Fatalf("failed to create scheduler: %v", err)
	}
	defer func() { _ = s.Shutdown() }()

	type scheduled struct {
		tab string
		job gocron.Job
	}
	var jobs []scheduled

	for _, tz := range []string{"Europe/Berlin", "UTC", "America/New_York"} {
		for _, weekdays := range [][]int{{0}, {6}, {1, 2, 3, 4, 5}, {0, 1, 2, 3, 4, 5, 6}} {
			sched := validSchedule()
			sched.Timezone = tz
			sched.Weekdays = weekdays

			job, err := s.NewJob(
				gocron.CronJob(sched.CronTab(), true),
				gocron.NewTask(func() {}),
			)
			if err != nil {
				t.Fatalf("gocron rejected crontab %q: %v", sched.CronTab(), err)
			}
			jobs = append(jobs, scheduled{tab: sched.CronTab(), job: job})
		}
	}

	// Next run times are only populated once the scheduler is running.
	s.Start()

	for _, j := range jobs {
		next, err := j.job.NextRun()
		if err != nil {
			t.Errorf("no next run for crontab %q: %v", j.tab, err)
			continue
		}
		if next.IsZero() {
			t.Errorf("next run for crontab %q is the zero time", j.tab)
		}
	}
}

// TestCronTabNextRunMatchesTimezone verifies the TZ= prefix is actually honoured
// rather than silently ignored, which is what makes per-schedule timezones work.
func TestCronTabNextRunMatchesTimezone(t *testing.T) {
	s, err := gocron.NewScheduler()
	if err != nil {
		t.Fatalf("failed to create scheduler: %v", err)
	}
	defer func() { _ = s.Shutdown() }()

	sched := validSchedule()
	sched.Timezone = "America/New_York"
	sched.Hour = 8
	sched.Minute = 30
	sched.Weekdays = []int{0, 1, 2, 3, 4, 5, 6}

	job, err := s.NewJob(gocron.CronJob(sched.CronTab(), true), gocron.NewTask(func() {}))
	if err != nil {
		t.Fatalf("failed to schedule: %v", err)
	}
	s.Start()

	next, err := job.NextRun()
	if err != nil {
		t.Fatalf("failed to get next run: %v", err)
	}

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("failed to load location: %v", err)
	}
	local := next.In(loc)
	if local.Hour() != 8 || local.Minute() != 30 {
		t.Errorf("next run is %s in New York, want 08:30", local.Format("15:04"))
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mut     func(*Schedule)
		wantErr bool
	}{
		{"valid wol", func(_ *Schedule) {}, false},
		{"empty name", func(s *Schedule) { s.Name = "  " }, true},
		{"unknown method", func(s *Schedule) { s.Method = "smoke-signal" }, true},
		{"wol cannot power off", func(s *Schedule) { s.Action = ActionOff }, true},
		{"invalid mac", func(s *Schedule) { s.MacAddress = "not-a-mac" }, true},
		{"invalid broadcast ip", func(s *Schedule) { s.BroadcastIP = "999.1.1.1" }, true},
		{"ipv6 broadcast rejected", func(s *Schedule) { s.BroadcastIP = "::1" }, true},
		{"valid broadcast ip", func(s *Schedule) { s.BroadcastIP = "192.168.1.255" }, false},
		{"hour too large", func(s *Schedule) { s.Hour = 24 }, true},
		{"hour negative", func(s *Schedule) { s.Hour = -1 }, true},
		{"minute too large", func(s *Schedule) { s.Minute = 60 }, true},
		{"no weekdays", func(s *Schedule) { s.Weekdays = []int{} }, true},
		{"weekday out of range", func(s *Schedule) { s.Weekdays = []int{7} }, true},
		{"invalid timezone", func(s *Schedule) { s.Timezone = "Mars/Olympus_Mons" }, true},
		{
			name: "atx may power off",
			mut: func(s *Schedule) {
				s.Method = MethodATX
				s.Action = ActionOff
				s.MacAddress = ""
			},
			wantErr: false,
		},
		{
			name: "atx may force power off",
			mut: func(s *Schedule) {
				s.Method = MethodATX
				s.Action = ActionOffForce
				s.MacAddress = ""
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := validSchedule()
			tt.mut(&s)
			err := s.Validate()
			if tt.wantErr && err == nil {
				t.Errorf("Validate() = nil, want an error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

// Validate doubles as a normaliser so the stored weekday list is stable.
func TestValidateNormalizesWeekdays(t *testing.T) {
	s := validSchedule()
	s.Weekdays = []int{5, 1, 5, 3, 1}

	if err := s.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	want := []int{1, 3, 5}
	if len(s.Weekdays) != len(want) {
		t.Fatalf("weekdays = %v, want %v", s.Weekdays, want)
	}
	for i := range want {
		if s.Weekdays[i] != want[i] {
			t.Fatalf("weekdays = %v, want %v", s.Weekdays, want)
		}
	}
}

func TestAllowedActions(t *testing.T) {
	if got := AllowedActions(MethodWOL); len(got) != 1 || got[0] != ActionOn {
		t.Errorf("AllowedActions(wol) = %v, want [on]", got)
	}
	if got := AllowedActions(MethodATX); len(got) != 3 {
		t.Errorf("AllowedActions(atx) = %v, want 3 actions", got)
	}
	if got := AllowedActions("nope"); got != nil {
		t.Errorf("AllowedActions(nope) = %v, want nil", got)
	}
}
