package selectors

import (
	"strconv"

	"github.com/alcionai/corso/src/internal/common"
	"github.com/alcionai/corso/src/pkg/backup/details"
	"github.com/alcionai/corso/src/pkg/filters"
)

// ---------------------------------------------------------------------------
// Selectors
// ---------------------------------------------------------------------------

type (
	// exchange provides an api for selecting
	// data scopes applicable to the Exchange service.
	exchange struct {
		Selector
	}

	// ExchangeBackup provides an api for selecting
	// data scopes applicable to the Exchange service,
	// plus backup-specific methods.
	ExchangeBackup struct {
		exchange
	}

	// ExchangeRestore provides an api for selecting
	// data scopes applicable to the Exchange service,
	// plus restore-specific methods.
	ExchangeRestore struct {
		exchange
	}
)

// NewExchange produces a new Selector with the service set to ServiceExchange.
func NewExchangeBackup() *ExchangeBackup {
	src := ExchangeBackup{
		exchange{
			newSelector(ServiceExchange),
		},
	}

	return &src
}

// ToExchangeBackup transforms the generic selector into an ExchangeBackup.
// Errors if the service defined by the selector is not ServiceExchange.
func (s Selector) ToExchangeBackup() (*ExchangeBackup, error) {
	if s.Service != ServiceExchange {
		return nil, badCastErr(ServiceExchange, s.Service)
	}

	src := ExchangeBackup{exchange{s}}

	return &src, nil
}

// NewExchangeRestore produces a new Selector with the service set to ServiceExchange.
func NewExchangeRestore() *ExchangeRestore {
	src := ExchangeRestore{
		exchange{
			newSelector(ServiceExchange),
		},
	}

	return &src
}

// ToExchangeRestore transforms the generic selector into an ExchangeRestore.
// Errors if the service defined by the selector is not ServiceExchange.
func (s Selector) ToExchangeRestore() (*ExchangeRestore, error) {
	if s.Service != ServiceExchange {
		return nil, badCastErr(ServiceExchange, s.Service)
	}

	src := ExchangeRestore{exchange{s}}

	return &src, nil
}

// -------------------
// Exclude/Includes

// Exclude appends the provided scopes to the selector's exclusion set.
// Every Exclusion scope applies globally, affecting all inclusion scopes.
// Data is excluded if it matches ANY exclusion (of the same data category).
//
// All parts of the scope must match for data to be exclucded.
// Ex: Mail(u1, f1, m1) => only excludes mail if it is owned by user u1,
// located in folder f1, and ID'd as m1.  MailSender(foo) => only excludes
// mail whose sender is foo.  Use selectors.Any() to wildcard a scope value.
// No value will match if selectors.None() is provided.
//
// Group-level scopes will automatically apply the Any() wildcard to
// child properties.
// ex: User(u1) automatically cascades to all mail, events, and contacts,
// therefore it is the same as selecting all of the following:
// Mail(u1, Any(), Any()), Event(u1, Any()), Contacts(u1, Any(), Any())
func (s *exchange) Exclude(scopes ...[]ExchangeScope) {
	s.Excludes = appendScopes(s.Excludes, scopes...)
}

// Filter appends the provided scopes to the selector's filters set.
// A selector with >0 filters and 0 inclusions will include any data
// that passes all filters.
// A selector with >0 filters and >0 inclusions will reduce the
// inclusion set to only the data that passes all filters.
// Data is retained if it passes ALL filters (of the same data category).
//
// All parts of the scope must match for data to pass the filter.
// Ex: Mail(u1, f1, m1) => only passes mail that is owned by user u1,
// located in folder f1, and ID'd as m1.  MailSender(foo) => only passes
// mail whose sender is foo.  Use selectors.Any() to wildcard a scope value.
// No value will match if selectors.None() is provided.
//
// Group-level scopes will automatically apply the Any() wildcard to
// child properties.
// ex: User(u1) automatically cascades to all mail, events, and contacts,
// therefore it is the same as selecting all of the following:
// Mail(u1, Any(), Any()), Event(u1, Any()), Contacts(u1, Any(), Any())
func (s *exchange) Filter(scopes ...[]ExchangeScope) {
	s.Filters = appendScopes(s.Filters, scopes...)
}

// Include appends the provided scopes to the selector's inclusion set.
// Data is included if it matches ANY inclusion.
// The inclusion set is later filtered (all included data must pass ALL
// filters) and excluded (all included data must not match ANY exclusion).
// Data is included if it matches ANY inclusion (of the same data category).
//
// All parts of the scope must match for data to be included.
// Ex: Mail(u1, f1, m1) => only includes mail if it is owned by user u1,
// located in folder f1, and ID'd as m1.  MailSender(foo) => only includes
// mail whose sender is foo.  Use selectors.Any() to wildcard a scope value.
// No value will match if selectors.None() is provided.
//
// Group-level scopes will automatically apply the Any() wildcard to
// child properties.
// ex: User(u1) automatically cascades to all mail, events, and contacts,
// therefore it is the same as selecting all of the following:
// Mail(u1, Any(), Any()), Event(u1, Any()), Contacts(u1, Any(), Any())
func (s *exchange) Include(scopes ...[]ExchangeScope) {
	s.Includes = appendScopes(s.Includes, scopes...)
}

// Scopes retrieves the list of exchangeScopes in the selector.
func (s *exchange) Scopes() []ExchangeScope {
	return scopes[ExchangeScope](s.Selector)
}

// DiscreteScopes retrieves the list of exchangeScopes in the selector.
// If any Include scope's User category is set to Any, replaces that
// scope's value with the list of userPNs instead.
func (s *exchange) DiscreteScopes(userPNs []string) []ExchangeScope {
	return discreteScopes[ExchangeScope](s.Selector, ExchangeUser, userPNs)
}

// -------------------
// Scope Factories

// Contacts produces one or more exchange contact scopes.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (s *exchange) Contacts(users, folders, contacts []string) []ExchangeScope {
	scopes := []ExchangeScope{}

	scopes = append(
		scopes,
		makeScope[ExchangeScope](Item, ExchangeContact, users, contacts).
			set(ExchangeContactFolder, folders),
	)

	return scopes
}

// Contactfolders produces one or more exchange contact folder scopes.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (s *exchange) ContactFolders(users, folders []string) []ExchangeScope {
	scopes := []ExchangeScope{}

	scopes = append(
		scopes,
		makeScope[ExchangeScope](Group, ExchangeContactFolder, users, folders),
	)

	return scopes
}

// Events produces one or more exchange event scopes.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (s *exchange) Events(users, calendars, events []string) []ExchangeScope {
	scopes := []ExchangeScope{}

	scopes = append(
		scopes,
		makeScope[ExchangeScope](Item, ExchangeEvent, users, events).
			set(ExchangeEventCalendar, calendars),
	)

	return scopes
}

// EventCalendars produces one or more exchange event calendar scopes.
// Calendars act as folders to contain Events
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (s *exchange) EventCalendars(users, events []string) []ExchangeScope {
	scopes := []ExchangeScope{}

	scopes = append(
		scopes,
		makeScope[ExchangeScope](Group, ExchangeEventCalendar, users, events),
	)

	return scopes
}

// Produces one or more mail scopes.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (s *exchange) Mails(users, folders, mails []string) []ExchangeScope {
	scopes := []ExchangeScope{}

	scopes = append(
		scopes,
		makeScope[ExchangeScope](Item, ExchangeMail, users, mails).
			set(ExchangeMailFolder, folders),
	)

	return scopes
}

// Produces one or more exchange mail folder scopes.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (s *exchange) MailFolders(users, folders []string) []ExchangeScope {
	scopes := []ExchangeScope{}

	scopes = append(
		scopes,
		makeScope[ExchangeScope](Group, ExchangeMailFolder, users, folders),
	)

	return scopes
}

// Produces one or more exchange contact user scopes.
// Each user id generates three scopes, one for each data type: contact, event, and mail.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (s *exchange) Users(users []string) []ExchangeScope {
	scopes := []ExchangeScope{}

	scopes = append(scopes,
		makeScope[ExchangeScope](Group, ExchangeContactFolder, users, Any()),
		makeScope[ExchangeScope](Item, ExchangeEventCalendar, users, Any()),
		makeScope[ExchangeScope](Group, ExchangeMailFolder, users, Any()),
	)

	return scopes
}

// -------------------
// Filter Factories

// ContactName produces one or more exchange contact name filter scopes.
// Matches any contact whose name contains the provided string.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (sr *ExchangeRestore) ContactName(senderID string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeContact,
			ExchangeFilterContactName,
			[]string{senderID},
			wrapFilter(filters.In)),
	}
}

// EventSubject produces one or more exchange event subject filter scopes.
// Matches any event where the event subject contains one of the provided strings.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (sr *ExchangeRestore) EventOrganizer(organizer string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeEvent,
			ExchangeFilterEventOrganizer,
			[]string{organizer},
			wrapFilter(filters.In)),
	}
}

// EventRecurs produces one or more exchange event recurrence filter scopes.
// Matches any event if the comparator flag matches the event recurrence flag.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (sr *ExchangeRestore) EventRecurs(recurs string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeEvent,
			ExchangeFilterEventRecurs,
			[]string{recurs},
			wrapFilter(filters.Equal)),
	}
}

// EventStartsAfter produces an exchange event starts-after filter scope.
// Matches any event where the start time is after the timestring.
// If the input equals selectors.Any, the scope will match all times.
// If the input is empty or selectors.None, the scope will always fail comparisons.
func (sr *ExchangeRestore) EventStartsAfter(timeStrings string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeMail,
			ExchangeFilterMailReceivedAfter,
			[]string{timeStrings},
			wrapFilter(filters.Less)),
	}
}

// EventStartsBefore produces an exchange event starts-before filter scope.
// Matches any event where the start time is before the timestring.
// If the input equals selectors.Any, the scope will match all times.
// If the input is empty or selectors.None, the scope will always fail comparisons.
func (sr *ExchangeRestore) EventStartsBefore(timeStrings string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeMail,
			ExchangeFilterMailReceivedBefore,
			[]string{timeStrings},
			wrapFilter(filters.Greater)),
	}
}

// EventSubject produces one or more exchange event subject filter scopes.
// Matches any event where the event subject contains one of the provided strings.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (sr *ExchangeRestore) EventSubject(subject string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeEvent,
			ExchangeFilterEventSubject,
			[]string{subject},
			wrapFilter(filters.In)),
	}
}

// MailReceivedAfter produces an exchange mail received-after filter scope.
// Matches any mail which was received after the timestring.
// If the input equals selectors.Any, the scope will match all times.
// If the input is empty or selectors.None, the scope will always fail comparisons.
func (sr *ExchangeRestore) MailReceivedAfter(timeStrings string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeMail,
			ExchangeFilterMailReceivedAfter,
			[]string{timeStrings},
			wrapFilter(filters.Less)),
	}
}

// MailReceivedBefore produces an exchange mail received-before filter scope.
// Matches any mail which was received before the timestring.
// If the input equals selectors.Any, the scope will match all times.
// If the input is empty or selectors.None, the scope will always fail comparisons.
func (sr *ExchangeRestore) MailReceivedBefore(timeStrings string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeMail,
			ExchangeFilterMailReceivedBefore,
			[]string{timeStrings},
			wrapFilter(filters.Greater)),
	}
}

// MailSender produces one or more exchange mail sender filter scopes.
// Matches any mail whose sender contains one of the provided strings.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (sr *ExchangeRestore) MailSender(sender string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeMail,
			ExchangeFilterMailSender,
			[]string{sender},
			wrapFilter(filters.In)),
	}
}

// MailSubject produces one or more exchange mail subject line filter scopes.
// Matches any mail whose subject contains one of the provided strings.
// If any slice contains selectors.Any, that slice is reduced to [selectors.Any]
// If any slice contains selectors.None, that slice is reduced to [selectors.None]
// If any slice is empty, it defaults to [selectors.None]
func (sr *ExchangeRestore) MailSubject(subject string) []ExchangeScope {
	return []ExchangeScope{
		makeFilterScope[ExchangeScope](
			ExchangeMail,
			ExchangeFilterMailSubject,
			[]string{subject},
			wrapFilter(filters.In)),
	}
}

// ---------------------------------------------------------------------------
// Destination
// ---------------------------------------------------------------------------

type ExchangeDestination Destination

func NewExchangeDestination() ExchangeDestination {
	return ExchangeDestination{}
}

// GetOrDefault gets the destination of the provided category.  If no
// destination is set, returns the current value.
func (d ExchangeDestination) GetOrDefault(cat exchangeCategory, current string) string {
	dest, ok := d[cat.String()]
	if !ok {
		return current
	}

	return dest.Target
}

// Sets the destination value of the provided category.  Returns an error
// if a destination is already declared for that category.
func (d ExchangeDestination) Set(cat exchangeCategory, dest string) error {
	if len(dest) == 0 {
		return nil
	}

	cs := cat.String()
	if curr, ok := d[cs]; ok {
		return existingDestinationErr(cs, curr.Target)
	}

	d[cs] = filterize(dest)

	return nil
}

// ---------------------------------------------------------------------------
// Categories
// ---------------------------------------------------------------------------

// exchangeCategory enumerates the type of the lowest level
// of data specified by the scope.
type exchangeCategory string

// interface compliance checks
var _ categorizer = ExchangeCategoryUnknown

const (
	ExchangeCategoryUnknown exchangeCategory = ""
	// types of data identified by exchange
	ExchangeContact       exchangeCategory = "ExchangeContact"
	ExchangeContactFolder exchangeCategory = "ExchangeContactFolder"
	ExchangeEvent         exchangeCategory = "ExchangeEvent"
	ExchangeEventCalendar exchangeCategory = "ExchangeEventCalendar"
	ExchangeMail          exchangeCategory = "ExchangeMail"
	ExchangeMailFolder    exchangeCategory = "ExchangeMailFolder"
	ExchangeUser          exchangeCategory = "ExchangeUser"
	// append new data cats here

	// filterable topics identified by exchange
	ExchangeFilterMailSender         exchangeCategory = "ExchangeFilterMailSender"
	ExchangeFilterMailSubject        exchangeCategory = "ExchangeFilterMailSubject"
	ExchangeFilterMailReceivedAfter  exchangeCategory = "ExchangeFilterMailReceivedAfter"
	ExchangeFilterMailReceivedBefore exchangeCategory = "ExchangeFilterMailReceivedBefore"
	ExchangeFilterContactName        exchangeCategory = "ExchangeFilterContactName"
	ExchangeFilterEventOrganizer     exchangeCategory = "ExchangeFilterEventOrganizer"
	ExchangeFilterEventRecurs        exchangeCategory = "ExchangeFilterEventRecurs"
	ExchangeFilterEventStartsAfter   exchangeCategory = "ExchangeFilterEventStartsAfter"
	ExchangeFilterEventStartsBefore  exchangeCategory = "ExchangeFilterEventStartsBefore"
	ExchangeFilterEventSubject       exchangeCategory = "ExchangeFilterEventSubject"
	// append new filter cats here
)

// exchangePathSet describes the category type keys used in Exchange paths.
// The order of each slice is important, and should match the order in which
// these types appear in the canonical Path for each type.
var exchangePathSet = map[categorizer][]categorizer{
	ExchangeContact: {ExchangeUser, ExchangeContactFolder, ExchangeContact},
	ExchangeEvent:   {ExchangeUser, ExchangeEventCalendar, ExchangeEvent},
	ExchangeMail:    {ExchangeUser, ExchangeMailFolder, ExchangeMail},
	ExchangeUser:    {ExchangeUser}, // the root category must be represented
}

func (ec exchangeCategory) String() string {
	return string(ec)
}

// leafCat returns the leaf category of the receiver.
// If the receiver category has multiple leaves (ex: User) or no leaves,
// (ex: Unknown), the receiver itself is returned.
// If the receiver category is a filter type (ex: ExchangeFilterMailSubject),
// returns the category covered by the filter.
// Ex: ExchangeContactFolder.leafCat() => ExchangeContact
// Ex: ExchangeEvent.leafCat() => ExchangeEvent
// Ex: ExchangeUser.leafCat() => ExchangeUser
func (ec exchangeCategory) leafCat() categorizer {
	switch ec {
	case ExchangeContact, ExchangeContactFolder, ExchangeFilterContactName:
		return ExchangeContact

	case ExchangeEvent, ExchangeEventCalendar, ExchangeFilterEventOrganizer, ExchangeFilterEventRecurs,
		ExchangeFilterEventStartsAfter, ExchangeFilterEventStartsBefore, ExchangeFilterEventSubject:
		return ExchangeEvent

	case ExchangeMail, ExchangeMailFolder, ExchangeFilterMailReceivedAfter,
		ExchangeFilterMailReceivedBefore, ExchangeFilterMailSender, ExchangeFilterMailSubject:
		return ExchangeMail
	}

	return ec
}

// rootCat returns the root category type.
func (ec exchangeCategory) rootCat() categorizer {
	return ExchangeUser
}

// unknownCat returns the unknown category type.
func (ec exchangeCategory) unknownCat() categorizer {
	return ExchangeCategoryUnknown
}

// transforms a path to a map of identified properties.
// TODO: this should use service-specific funcs in the Paths pkg.  Instead of
// peeking at the path directly, the caller should compare against values like
// path.UserPN() and path.Folders().
//
// Malformed (ie, short len) paths will return incomplete results.
// Example:
// [tenantID, userPN, "mail", mailFolder, mailID]
// => {exchUser: userPN, exchMailFolder: mailFolder, exchMail: mailID}
func (ec exchangeCategory) pathValues(path []string) map[categorizer]string {
	m := map[categorizer]string{}
	if len(path) < 6 {
		return m
	}

	m[ExchangeUser] = path[2]

	switch ec {
	case ExchangeContact:
		m[ExchangeContactFolder] = path[4]
		m[ExchangeContact] = path[5]

	case ExchangeEvent:
		m[ExchangeEventCalendar] = path[4]
		m[ExchangeEvent] = path[5]

	case ExchangeMail:
		m[ExchangeMailFolder] = path[4]
		m[ExchangeMail] = path[5]
	}

	return m
}

// pathKeys returns the path keys recognized by the receiver's leaf type.
func (ec exchangeCategory) pathKeys() []categorizer {
	return exchangePathSet[ec.leafCat()]
}

// ---------------------------------------------------------------------------
// Scopes
// ---------------------------------------------------------------------------

// ExchangeScope specifies the data available
// when interfacing with the Exchange service.
type ExchangeScope scope

// interface compliance checks
var _ scoper = &ExchangeScope{}

// Category describes the type of the data in scope.
func (s ExchangeScope) Category() exchangeCategory {
	return exchangeCategory(getCategory(s))
}

// categorizer type is a generic wrapper around Category.
// Primarily used by scopes.go to for abstract comparisons.
func (s ExchangeScope) categorizer() categorizer {
	return s.Category()
}

// Matches returns true if the category is included in the scope's
// data type, and the target string matches that category's comparator.
func (s ExchangeScope) Matches(cat exchangeCategory, target string) bool {
	return matches(s, cat, target)
}

// FilterCategory returns the category enum of the scope filter.
// If the scope is not a filter type, returns ExchangeUnknownCategory.
func (s ExchangeScope) FilterCategory() exchangeCategory {
	return exchangeCategory(getFilterCategory(s))
}

// Granularity describes the granularity (directory || item)
// of the data in scope.
func (s ExchangeScope) Granularity() string {
	return getGranularity(s)
}

// IncludeCategory checks whether the scope includes a certain category of data.
// Ex: to check if the scope includes mail data:
// s.IncludesCategory(selector.ExchangeMail)
func (s ExchangeScope) IncludesCategory(cat exchangeCategory) bool {
	return categoryMatches(s.Category(), cat)
}

// returns true if the category is included in the scope's data type,
// and the value is set to Any().
func (s ExchangeScope) IsAny(cat exchangeCategory) bool {
	return isAnyTarget(s, cat)
}

// Get returns the data category in the scope.  If the scope
// contains all data types for a user, it'll return the
// ExchangeUser category.
func (s ExchangeScope) Get(cat exchangeCategory) []string {
	return getCatValue(s, cat)
}

// sets a value by category to the scope.  Only intended for internal use.
func (s ExchangeScope) set(cat exchangeCategory, v []string) ExchangeScope {
	return set(s, cat, v)
}

// setDefaults ensures that contact folder, mail folder, and user category
// scopes all express `AnyTgt` for their child category types.
func (s ExchangeScope) setDefaults() {
	switch s.Category() {
	case ExchangeContactFolder:
		s[ExchangeContact.String()] = passAny

	case ExchangeEventCalendar:
		s[ExchangeEvent.String()] = passAny

	case ExchangeMailFolder:
		s[ExchangeMail.String()] = passAny

	case ExchangeUser:
		s[ExchangeContactFolder.String()] = passAny
		s[ExchangeContact.String()] = passAny
		s[ExchangeEvent.String()] = passAny
		s[ExchangeMailFolder.String()] = passAny
		s[ExchangeMail.String()] = passAny
	}
}

// ---------------------------------------------------------------------------
// Backup Details Filtering
// ---------------------------------------------------------------------------

// Reduce filters the entries in a details struct to only those that match the
// inclusions, filters, and exclusions in the selector.
func (s exchange) Reduce(deets *details.Details) *details.Details {
	return reduce[ExchangeScope](
		deets,
		s.Selector,
		map[pathType]exchangeCategory{
			exchangeContactPath: ExchangeContact,
			exchangeEventPath:   ExchangeEvent,
			exchangeMailPath:    ExchangeMail,
		},
	)
}

// matchesEntry returns true if either the path or the info in the exchangeEntry matches the scope details.
func (s ExchangeScope) matchesEntry(
	cat categorizer,
	pathValues map[categorizer]string,
	entry details.DetailsEntry,
) bool {
	// matchesPathValues can be handled generically, thanks to SCIENCE.
	return matchesPathValues(s, cat.(exchangeCategory), pathValues) || s.matchesInfo(entry.Exchange)
}

// matchesInfo handles the standard behavior when comparing a scope and an ExchangeFilter
// returns true if the scope and info match for the provided category.
func (s ExchangeScope) matchesInfo(info *details.ExchangeInfo) bool {
	// we need values to match against
	if info == nil {
		return false
	}

	filterCat := s.FilterCategory()
	i := ""

	switch filterCat {
	case ExchangeFilterContactName:
		i = info.ContactName
	case ExchangeFilterEventOrganizer:
		i = info.Organizer
	case ExchangeFilterEventRecurs:
		i = strconv.FormatBool(info.EventRecurs)
	case ExchangeFilterEventStartsAfter, ExchangeFilterEventStartsBefore:
		i = common.FormatTime(info.EventStart)
	case ExchangeFilterEventSubject:
		i = info.Subject
	case ExchangeFilterMailSender:
		i = info.Sender
	case ExchangeFilterMailSubject:
		i = info.Subject
	case ExchangeFilterMailReceivedAfter, ExchangeFilterMailReceivedBefore:
		i = common.FormatTime(info.Received)
	}

	return s.Matches(filterCat, i)
}
