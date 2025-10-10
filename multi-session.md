# JetKVM Multi-Session Support

## Table of Contents

1. [Overview](#overview)
2. [Session Modes](#session-modes)
3. [Session Settings](#session-settings)
4. [Session Lifecycle](#session-lifecycle)
5. [Grace Period & Reconnection](#grace-period--reconnection)
6. [Session Approval System](#session-approval-system)
7. [Primary Role Transfer](#primary-role-transfer)
8. [Emergency Promotion](#emergency-promotion)
9. [Permissions System](#permissions-system)
10. [Session Identification](#session-identification)
11. [Testing Guide](#testing-guide)

---

## Overview

JetKVM supports multiple concurrent connections to a single device, allowing multiple users to collaborate or observe remote system management. The multi-session system uses a role-based access control model with automatic session management and conflict resolution.

### Key Features

- **Multiple concurrent connections** - Up to 10 simultaneous sessions per device
- **Role-based permissions** - Four distinct session modes with different privilege levels
- **Automatic session promotion** - Seamless handoff between primary sessions
- **Grace period reconnection** - Recover from accidental disconnects without losing control
- **Session approval workflow** - Optional gating for new connections
- **Transfer protection** - Prevent unwanted session takeovers
- **Automatic nickname generation** - Identify sessions by browser type

### Design Philosophy

The multi-session system is designed with the following principles:

1. **Always have a primary** - The system ensures at least one session has control when sessions exist
2. **Graceful degradation** - Accidental disconnects are protected by grace periods
3. **Manual control prioritized** - User-initiated actions take precedence over automatic promotion
4. **Security by default** - Emergency promotions use trust scoring to select the most appropriate session

---

## Session Modes

Every session operates in one of four modes, each with different permissions and responsibilities.

### Primary Mode

![Primary Session Screenshot - placeholder for image]

**Permissions**: Full control of the remote system

The primary session has complete control over the KVM device and can:
- View video feed
- Send keyboard and mouse input
- Control power (ATX/DC)
- Mount/unmount virtual media
- Access all system settings
- Manage other sessions (approve, deny, transfer, kick)
- Access serial console and terminal
- Configure USB devices and extensions

**Visual Indicators**:
- Primary badge displayed in session list
- Full control UI enabled
- All menu options accessible

**Automatic Behaviors**:
- Only one primary session exists at any time
- Primary session receives exclusive input control
- Times out after 5 minutes of inactivity (configurable)
- Can voluntarily release control to observers

### Observer Mode

![Observer Session Screenshot - placeholder for image]

**Permissions**: View-only access with request capability

Observer sessions can:
- View video feed in real-time
- See mounted media (but cannot mount/unmount)
- Request primary control
- View session list

Observer sessions **cannot**:
- Send keyboard/mouse input
- Control power or hardware
- Modify system settings
- Manage other sessions

**Visual Indicators**:
- "Request Control" button visible
- Input controls disabled/grayed out
- Settings menu locked

**Automatic Behaviors**:
- Promoted to primary when no primary exists (if not blacklisted)
- Can be promoted by current primary via manual transfer
- Automatically becomes observer if primary is transferred away

### Queued Mode

![Queued Session Screenshot - placeholder for image]

**Permissions**: Same as observer, with pending request status

Queued sessions have:
- Same permissions as observers
- Visible indication that control has been requested
- Position in request queue

**Visual Indicators**:
- "Request Pending" status shown
- Queue position displayed (if multiple requests)
- "Cancel Request" button available

**Automatic Behaviors**:
- Enters this mode after clicking "Request Control"
- Primary session is notified of the request
- Returns to observer if request is denied
- Becomes primary if request is approved

### Pending Mode

![Pending Session Screenshot - placeholder for image]

**Permissions**: No access until approved

Pending sessions have:
- **No video access** - Screen shows "Waiting for approval"
- **No system access** - Cannot view or interact with anything
- Optional nickname field (if required by settings)

**Visual Indicators**:
- "Waiting for approval" message
- Nickname input field (if required)
- No video feed or controls visible

**Automatic Behaviors**:
- Only active when "Require Approval" setting is enabled
- Session must be approved by current primary
- Becomes observer after approval
- Automatically removed after 1 minute if not approved (security measure)

---

## Session Settings

Session behavior is controlled through global settings that affect all current and future sessions.

### Require Approval

**Default**: `false`
**Type**: Boolean

When enabled, new sessions must be explicitly approved by the current primary session before gaining access.

**Use Cases**:
- Secure environments where unauthorized viewing should be prevented
- Limiting access to trusted users only
- Preventing session spam or unauthorized monitoring

**Behavior**:
- New sessions start in Pending mode with no video access
- Primary receives notification with session details (source, identity, nickname)
- Primary can approve or deny access
- If no primary exists, the system uses emergency promotion to select a trusted session

**Security Note**: This setting prevents unauthorized video access but requires a primary session to approve new connections. If the primary disconnects, the system will automatically promote the most trusted pending session to prevent deadlock.

### Require Nickname

**Default**: `false`
**Type**: Boolean

When enabled, sessions must provide a valid nickname before being approved.

**Use Cases**:
- Identify multiple concurrent users
- Maintain audit trail of session activity
- Prevent anonymous connections

**Behavior**:
- Sessions without nicknames cannot be approved
- Approval request is delayed until nickname is provided
- Nickname must be 2-30 characters, alphanumeric with dashes/underscores
- If disabled, nicknames are auto-generated based on browser type

**Auto-Generated Nicknames**:
When nickname requirement is disabled, sessions receive automatic nicknames in the format:
- `u-chrome-a1b2` - Chrome browser
- `u-firefox-c3d4` - Firefox browser
- `u-safari-e5f6` - Safari browser
- `u-edge-g7h8` - Edge browser
- `u-user-i9j0` - Unknown browser

### Reconnect Grace Period

**Default**: `10` seconds
**Type**: Integer (1-300 seconds)
**Configurable**: Yes

Grace period duration for primary session reconnection after disconnect.

**Purpose**:
Prevents accidental loss of control due to:
- Network hiccups
- Browser tab refresh
- Page navigation
- Temporary connection issues

**Behavior**:
- Primary session disconnects → grace period starts
- Primary slot is reserved for the disconnected session
- Other sessions cannot become primary during grace period
- Original session can reclaim primary status if reconnecting within grace period
- If grace period expires without reconnection, next eligible session is promoted

**Technical Details**:
- Grace period is cleared on intentional logout
- Manual transfers bypass grace period (immediate promotion)
- Grace period does not apply to observer sessions (they reconnect as observers)

### Primary Timeout

**Default**: `300` seconds (5 minutes)
**Type**: Integer
**Configurable**: Yes
**Special Values**: `0` = disabled (no timeout)

Inactivity timeout for primary session.

**Purpose**:
- Prevent abandoned sessions from holding control indefinitely
- Free up primary slot when user walks away
- Ensure active users have access to control

**Activity Tracking**:
Primary session is considered "active" when:
- Mouse movement detected
- Keyboard input sent
- Any RPC method called
- WebRTC keep-alive ping received

**Behavior**:
- Timer resets on each activity
- After timeout expires, primary is demoted to observer
- Next eligible session is automatically promoted to primary
- Original session can request control again after demotion

### Private Keystrokes

**Default**: `false`
**Type**: Boolean

When enabled, keystroke events are only broadcast to the primary session.

**Purpose**:
- Prevent observer sessions from seeing typed passwords
- Protect sensitive data entry
- Maintain privacy during credential input

**Behavior**:
- When `false`: All sessions see keystroke notifications (for awareness)
- When `true`: Only primary session receives keystroke events
- Mouse movements are always visible to all sessions
- Does not affect actual input execution (only event visibility)

**Use Cases**:
- Password entry during login
- Entering API keys or tokens
- Private configuration data
- Any sensitive text input

### Maximum Rejection Attempts

**Default**: `3`
**Type**: Integer (1-10)

Maximum number of times a session can be rejected before automatic blocking.

**Purpose**:
- Prevent spam from repeatedly requesting approval
- Rate limit approval request DoS attacks
- Automatic blacklisting of problematic sessions

**Behavior**:
- Counter increments on each denial
- After reaching limit, session is automatically disconnected
- Counter resets after 60 seconds of no requests
- Does not affect legitimate new connection attempts

---

## Session Lifecycle

### Connection Establishment

```
[Browser] → [WebRTC Handshake] → [Session Creation] → [Mode Assignment]
```

1. **Initial Connection**
   - Client connects via WebRTC
   - Session ID generated (UUID)
   - Source and identity extracted from connection metadata
   - Browser type detected from User-Agent

2. **Mode Assignment Logic**
   ```
   IF session exists in grace period:
       Reconnect to existing session (preserve mode)
   ELSE IF no primary exists AND not blacklisted:
       Assign Primary mode
   ELSE IF approval required AND primary exists:
       Assign Pending mode
   ELSE:
       Assign Observer mode
   ```

3. **Validation & Broadcasting**
   - Session added to manager
   - Single primary verified (auto-fix if multiple)
   - All sessions notified of new connection
   - Primary notified if approval required

### Normal Disconnection

```
[User Logout] → [Clear Grace Period] → [Remove Session] → [Promote Next]
```

**Intentional Logout Flow**:
1. Session calls logout/disconnect method
2. Grace period is cleared (marked as intentional)
3. Session removed from manager
4. If was primary: immediate promotion of next eligible session
5. All sessions notified of removal

**Characteristics**:
- No grace period applied
- Immediate promotion if primary leaves
- Clean session cleanup
- No reconnection possibility

### Accidental Disconnection

```
[Connection Lost] → [Grace Period Active] → [Await Reconnect | Timeout]
                         ↓                           ↓
                [Reconnect Success]          [Promote Next]
```

**Accidental Disconnect Flow**:
1. Connection drops (network issue, browser issue, etc.)
2. Grace period started (default 10 seconds)
3. Session info preserved in reconnection map
4. Primary slot reserved (if was primary)
5. **Wait for reconnection**:
   - If reconnects within grace: restore original mode
   - If grace expires: session removed, next promoted

**Grace Period Details**:
- Grace period duration configurable (1-300 seconds)
- Maximum 10 concurrent grace periods (DoS protection)
- Oldest entries evicted if limit reached
- Grace periods cleared on new primary establishment

### Reconnection Scenarios

#### Scenario 1: Primary Reconnects Within Grace Period

```
Time 0s:  Primary disconnects (network issue)
Time 0s:  Grace period starts (10 seconds)
Time 3s:  Primary reconnects → RESTORED as primary ✓
```

**Result**: Session seamlessly reclaims primary status, no interruption to workflow.

#### Scenario 2: Primary Reconnects After Grace Expiry

```
Time 0s:  Primary disconnects
Time 0s:  Grace period starts (10 seconds)
Time 12s: Grace expires → Observer B promoted to primary
Time 15s: Original primary reconnects → Becomes observer ✗
```

**Result**: Original primary returns as observer, must request control if needed.

#### Scenario 3: Observer Reconnects

```
Time 0s:  Observer disconnects
Time 0s:  Grace period starts
Time 5s:  Observer reconnects → RESTORED as observer ✓
```

**Result**: Observer sessions always reconnect as observers (no primary slot reservation).

#### Scenario 4: Multiple Rapid Disconnects

```
Time 0s:  Primary A disconnects → Grace period active
Time 1s:  Observer B promoted (emergency)
Time 2s:  Primary B disconnects → Grace period active
Time 3s:  Observer C promoted (emergency, rate limit bypassed)
```

**Result**: System ensures at least one primary always exists, bypassing rate limits when necessary.

---

## Grace Period & Reconnection

### Grace Period Mechanics

The grace period is a time window during which a disconnected session can reclaim its previous role without interruption.

**Key Properties**:
- **Per-Session**: Each session gets its own grace period
- **Mode-Aware**: Different behavior for primary vs observer sessions
- **Expiration-Based**: Time-bound protection window
- **Blacklist-Aware**: Respects manual transfer blacklisting

### Primary Grace Period

When a primary session disconnects accidentally:

1. **Grace Period Activation**
   ```go
   sm.primarySessionID = ""              // Clear active slot
   sm.lastPrimaryID = sessionID           // Remember who it was
   sm.reconnectGrace[sessionID] = now + 10s  // Set expiration
   ```

2. **Protection During Grace**
   - Primary slot remains empty (no one can claim it)
   - `lastPrimaryID` tracks the rightful owner
   - Other sessions cannot be promoted to primary
   - Requests are queued instead of immediately granted

3. **Successful Reconnection**
   ```go
   IF session.ID == sm.lastPrimaryID AND not blacklisted:
       session.Mode = Primary
       sm.primarySessionID = session.ID
       sm.lastPrimaryID = ""  // Clear tracking
   ```

4. **Grace Expiration**
   - After 10 seconds (configurable), grace period expires
   - System promotes most eligible observer to primary
   - If approval required, uses trust-based emergency promotion
   - Original session becomes observer if it reconnects later

### Observer Grace Period

When an observer session disconnects:

1. **Grace Period Activation**
   - Same as primary, but no primary slot reservation
   - Session info stored in reconnection map
   - No impact on other sessions

2. **Successful Reconnection**
   - Restores as observer
   - No promotion or special treatment
   - Session resumes viewing

3. **Grace Expiration**
   - Session removed from grace map
   - No promotion occurs
   - Session gone forever unless new connection made

### Blacklist Interaction

**Transfer Blacklisting** (60-second duration):
When a manual transfer occurs (A → B):
1. Session A is demoted to observer
2. Session A is blacklisted for 60 seconds
3. All other sessions (except B) are blacklisted for 60 seconds
4. Grace periods for blacklisted sessions are cleared

**Purpose**: Prevent immediate re-takeover after manual transfer

**Grace Period Impact**:
- Blacklisted sessions cannot reclaim primary during grace period
- Reconnection still works, but session becomes observer instead
- Blacklist expires after 60 seconds, then normal grace period rules apply

### Grace Period Edge Cases

#### Case 1: Grace Period During Manual Transfer

```
Time 0s: Primary A disconnects → Grace active
Time 3s: Observer B manually requests control
Time 3s: Primary A's grace cleared, B promoted
Time 5s: Primary A reconnects → Becomes observer (blacklisted)
```

**Reason**: Manual user action takes precedence over grace period.

#### Case 2: Multiple Grace Periods

```
Primary A disconnects → Grace A active (expires at T+10s)
Observer B disconnects → Grace B active (expires at T+10s)
Observer C disconnects → Grace C active (expires at T+10s)
```

**Limit**: Maximum 10 concurrent grace periods (DoS protection)
**Eviction**: Oldest grace period removed if limit exceeded

#### Case 3: Grace Period with Approval Required

```
Primary disconnects → Grace active
New session connects → Becomes pending (no primary to approve)
Grace expires → Pending session promoted via emergency promotion
```

**Reason**: System must always have a primary when sessions exist.

---

## Session Approval System

The approval system gates new connections, requiring explicit permission from the current primary before granting access.

### Approval Workflow

![Approval Workflow Diagram - placeholder for image]

```
[New Session] → [Pending Mode] → [Primary Notified] → [Approve/Deny] → [Observer/Disconnect]
```

#### Step 1: New Session Arrives

```go
IF RequireApproval enabled AND primary exists:
    session.Mode = Pending
    session.hasPermission(VideoView) = false  // No video access
```

**Result**: Session sees "Waiting for approval" screen, no video feed.

#### Step 2: Primary Notification

Primary session receives JSON-RPC event:
```json
{
  "method": "newSessionPending",
  "params": {
    "sessionId": "uuid-here",
    "source": "cloud" | "local",
    "identity": "user@example.com",
    "nickname": "user-chrome-a1b2"
  }
}
```

**UI Display**:
- Notification badge appears
- Session list shows pending session with "Approve" and "Deny" buttons
- Session details visible (source, nickname)

#### Step 3: Nickname Validation (if required)

```go
IF RequireNickname enabled AND nickname empty:
    // Wait for nickname before showing approval request
    return
```

**Behavior**:
- Approval request delayed until nickname provided
- Pending session shows nickname input field
- After nickname entered, approval request sent to primary

#### Step 4: Primary Decision

**Approve Action**:
```json
{
  "method": "approveNewSession",
  "params": {
    "sessionId": "uuid-here"
  }
}
```

**Result**:
- Session promoted to Observer mode
- Video access granted
- Session can now view and request control
- All sessions notified of mode change

**Deny Action**:
```json
{
  "method": "denyNewSession",
  "params": {
    "sessionId": "uuid-here"
  }
}
```

**Result**:
- Rejection counter incremented
- Session receives "Access Denied" message
- Connection closed after 5 seconds
- If rejection counter exceeds max (default 3), session is blocked

### Approval Security

#### DoS Protection

**Rate Limiting**:
- Maximum 3 rejections per session (configurable)
- After reaching limit, session auto-disconnected
- 60-second cooldown before counter resets

**Pending Session Timeout**:
- Pending sessions removed after 1 minute if not approved
- Prevents accumulation of stale pending sessions
- Logged as "timed-out pending session"

**Maximum Pending Sessions**:
- System tracks and limits pending session count
- Oldest pending sessions removed if limit reached

#### Emergency Approval Bypass

**Scenario**: Primary disconnects while sessions are pending

```
Primary disconnects → Grace expires → No primary exists
BUT pending sessions exist → DEADLOCK RISK
```

**Solution**: Emergency promotion with approval bypass

```go
IF no primary exists AND approval required:
    // Find most trusted pending/observer session
    session = findMostTrustedSessionForEmergency()
    session.Mode = Primary  // Bypass approval requirement
    LogWarning("EMERGENCY: Bypassing approval to prevent deadlock")
```

**Trust Scoring**:
Sessions are scored based on:
- Session age (longer = more trusted, up to 100 points)
- Previous primary status (+50 points)
- Current mode (observer +20, queued +10, pending +0)
- Nickname presence (if required: +15 if present, -30 if missing)

**Highest Score Wins**: Most trusted session promoted to prevent deadlock.

### Approval Testing Scenarios

#### Test 1: Basic Approval Flow

1. Enable "Require Approval"
2. Connect Session A → Becomes primary
3. Connect Session B → Becomes pending
4. Session B shows "Waiting for approval", no video
5. Session A receives notification
6. Session A approves → Session B becomes observer with video access

**Expected**: Clean approval flow, video access granted after approval.

#### Test 2: Approval with Nickname Required

1. Enable "Require Approval" and "Require Nickname"
2. Connect Session A → Becomes primary
3. Connect Session B (no nickname) → Becomes pending
4. Session B shows nickname input field
5. Session A does NOT receive notification (waiting for nickname)
6. Session B enters nickname "TestUser"
7. Session A NOW receives notification
8. Session A approves → Session B becomes observer

**Expected**: Approval delayed until nickname provided.

#### Test 3: Approval Denial

1. Enable "Require Approval"
2. Connect Session A → Becomes primary
3. Connect Session B → Becomes pending
4. Session A denies → Session B disconnected
5. Session B reconnects → Becomes pending again
6. Session A denies → Rejection counter = 2
7. Session B reconnects → Becomes pending again
8. Session A denies → Session B blocked (max rejections reached)

**Expected**: After 3 rejections, session auto-blocked.

#### Test 4: Emergency Approval Bypass

1. Enable "Require Approval"
2. Connect Session A → Becomes primary
3. Connect Session B → Becomes pending (no video)
4. Session A disconnects (close browser)
5. Grace period starts (10 seconds)
6. Wait for grace expiration
7. Session B automatically promoted to primary (emergency bypass)

**Expected**: Session B gains primary status despite pending mode.

---

## Primary Role Transfer

The system supports multiple ways to transfer primary control between sessions.

### Transfer Types

#### 1. Direct Transfer (Manual)

**Initiated By**: Current primary session
**Target**: Specific observer or queued session
**Method**: `transferPrimary(fromID, toID)`

**Flow**:
```
Primary A → "Transfer to Session B" → Direct transfer
```

**Behavior**:
1. Session A demoted to observer
2. Session A blacklisted for 60 seconds
3. Session B promoted to primary
4. Session B blacklist entry removed
5. All other sessions blacklisted for 60 seconds
6. Grace periods cleared
7. `lastPrimaryID` cleared (prevents reconnection as primary)

**Use Case**: Primary voluntarily gives control to specific session.

**UI**: "Transfer Control" button next to observer sessions.

#### 2. Approval Transfer

**Initiated By**: Queued session requesting control
**Target**: Requesting session
**Method**: `approveRequest(requesterID)`

**Flow**:
```
Observer B → "Request Control" → Queued → Primary A approves → Transfer
```

**Behavior**:
- Same as direct transfer
- Requester removed from queue
- Primary receives notification of request
- Approval UI shown in session list

**Use Case**: Observer asks for control, primary grants it.

#### 3. Release Transfer

**Initiated By**: Current primary session
**Target**: Next eligible observer
**Method**: `releasePrimary()`

**Flow**:
```
Primary A → "Release Control" → Find next observer → Auto-promote
```

**Behavior**:
1. Primary A demoted to observer
2. System finds next eligible session (not blacklisted)
3. Selected session promoted to primary
4. Blacklist applied to protect new primary
5. Queue order respected (queued sessions first)

**Use Case**: Primary gives up control without specifying recipient.

**UI**: "Release Control" button in session menu.

#### 4. Emergency Promotion

**Initiated By**: System (automatic)
**Target**: Most eligible/trusted session
**Trigger**: Primary timeout, grace expiration, or deadlock

**Types of Emergency Promotion**:

**a) Emergency Auto-Promotion** (`emergency_auto_promotion`)
- Occurs when no primary exists and no grace period active
- Triggered by validation checks
- Selects any eligible observer (if approval not required)
- Uses trust scoring if approval required

**b) Emergency Timeout Promotion** (`emergency_timeout_promotion`)
- Occurs when primary times out due to inactivity
- Primary timeout default: 5 minutes
- Trust scoring used if approval required
- Excludes timed-out session from promotion

**c) Emergency Deadlock Prevention** (`emergency_promotion_deadlock_prevention`)
- Occurs when grace period expires without reconnection
- Prevents system from having no primary
- Trust scoring used if approval required
- Rate limited (30 seconds between emergency promotions)

**Rate Limiting**:
```go
IF no primary exists:
    Bypass all rate limits  // CRITICAL: Must always have primary
ELSE:
    IF last emergency < 30 seconds ago:
        Block promotion (potential DoS attack)
    IF consecutive emergencies >= 3:
        Block promotion (security protection)
```

**Trust Scoring Algorithm**:
```
score = 0
score += min(sessionAge.Minutes(), 100)  // Up to 100 points for age
IF lastPrimaryID == sessionID:
    score += 50  // Previous primary bonus
IF mode == Observer:
    score += 20
ELSE IF mode == Queued:
    score += 10
IF nickname required AND nickname present:
    score += 15
IF nickname required AND nickname missing:
    score -= 30
```

### Transfer Protection (Blacklisting)

**Purpose**: Prevent unwanted immediate re-takeover after manual transfer.

**Duration**: 60 seconds

**Applied To**: All sessions except newly promoted primary

**Mechanism**:
```go
type TransferBlacklistEntry struct {
    SessionID string
    ExpiresAt time.Time
}
```

**Only Applied During Manual Transfers**:
- Direct transfer
- Approval transfer
- Release transfer

**NOT Applied During Emergency Promotions**:
- Emergency auto-promotion
- Emergency timeout promotion
- Emergency deadlock prevention
- Initial promotion (first session)

**Reasoning**:
- Manual transfers = user-initiated, need protection from immediate reversal
- Emergency promotions = system-initiated for availability, must happen immediately

**Effects**:
- Blacklisted sessions cannot become primary
- Blacklisted sessions cannot be selected for promotion
- Grace period reconnection respects blacklist
- Expires after 60 seconds automatically

### Transfer Testing Scenarios

#### Test 1: Direct Transfer

1. Session A is primary, Session B is observer
2. Session A clicks "Transfer to Session B"
3. Session A becomes observer
4. Session B becomes primary
5. Session A is blacklisted for 60 seconds
6. Session A tries to request control → Blocked by blacklist
7. Wait 60 seconds
8. Session A requests control → Allowed

**Expected**: Clean transfer with 60-second protection.

#### Test 2: Transfer with Refresh

1. Session A is primary, Session B is observer
2. Session A clicks "Transfer to Session B"
3. Session B becomes primary
4. Session B refreshes browser (WebRTC reconnection)
5. Session B reconnects and REMAINS primary

**Expected**: Session B does not lose primary status on refresh.

#### Test 3: Primary Logout with Observers

1. Session A is primary, Sessions B and C are observers
2. Session A clicks "Logout"
3. Session A disconnects (intentional)
4. Session B OR C promoted immediately (no grace period)

**Expected**: Instant promotion, no delay.

#### Test 4: Primary Timeout

1. Session A is primary, Session B is observer
2. Session A becomes inactive (no input for 5 minutes)
3. After 5 minutes, Session A demoted to observer
4. Session B promoted to primary automatically
5. Session A can request control again

**Expected**: Automatic handoff on timeout.

---

## Emergency Promotion

Emergency promotion is the system's safety mechanism to ensure there is always a primary session when sessions exist.

### When Emergency Promotion Occurs

#### Trigger 1: No Primary Exists (Validation)

**Scenario**: System detects no primary during periodic validation

```go
// Runs every 10 seconds
func validateSinglePrimary() {
    IF primaryCount == 0 AND totalSessions > 0 AND no active grace period:
        EmergencyPromote()
}
```

**Common Causes**:
- Bug in session management
- Race condition during transfers
- Manual state corruption

**Resolution**: Automatic promotion of next eligible session.

#### Trigger 2: Primary Timeout

**Scenario**: Primary session inactive for too long (default 5 minutes)

```go
IF now - primarySession.LastActive > 5 minutes:
    DemotePrimary()
    EmergencyPromote()
```

**Activity Resets Timer**:
- Mouse movement
- Keyboard input
- Any RPC method call
- WebRTC ping/pong

**Resolution**: Timed-out session demoted, next session promoted.

#### Trigger 3: Grace Period Expiration

**Scenario**: Primary disconnects, grace period expires without reconnection

```go
IF grace period expired AND lastPrimaryID set:
    ClearPrimarySlot()
    EmergencyPromote()
```

**Timeline**:
```
T+0s:  Primary disconnects
T+0s:  Grace period starts (10 seconds)
T+10s: Grace expires
T+10s: Emergency promotion triggered
```

**Resolution**: Most eligible session promoted to prevent indefinite wait.

#### Trigger 4: Multiple Rapid Disconnects

**Scenario**: Primary and newly promoted session both disconnect quickly

```
T+0s:  Primary A disconnects
T+1s:  Observer B promoted (emergency)
T+2s:  Primary B disconnects (in background tab, ICE gathering stuck)
T+2s:  Observer C promoted (emergency, rate limit bypassed)
```

**Critical Fix**: Rate limits bypassed when no primary exists

```go
hasPrimary := sm.primarySessionID != ""
IF !hasPrimary:
    LogError("CRITICAL: No primary exists - bypassing all rate limits")
    // Promote immediately, ignore rate limits
```

**Reasoning**: System availability takes precedence over rate limiting when deadlock is imminent.

### Emergency Promotion Selection

#### Without Approval Requirement

**Algorithm**: Simple first-eligible selection

```go
// Check queue first
IF queueOrder not empty:
    FOR each queued session:
        IF not blacklisted:
            RETURN session

// Then check observers
FOR each session WHERE mode == Observer:
    IF not blacklisted:
        RETURN session

// Last resort: pending sessions
FOR each session WHERE mode == Pending:
    IF not blacklisted:
        RETURN session
```

**Priority Order**:
1. Queued sessions (in queue order)
2. Observer sessions (arbitrary order)
3. Pending sessions (last resort)

#### With Approval Requirement

**Algorithm**: Trust-based scoring

```go
bestSessionID = ""
bestScore = -1

// First pass: Observers and Queued
FOR each session WHERE mode IN [Observer, Queued]:
    IF not blacklisted:
        score = calculateTrustScore(session)
        IF score > bestScore:
            bestScore = score
            bestSessionID = session.ID

// Second pass: Pending (only if no observers found)
IF bestSessionID == "":
    FOR each session WHERE mode == Pending:
        IF not blacklisted:
            score = calculateTrustScore(session)
            IF score > bestScore:
                bestScore = score
                bestSessionID = session.ID
```

**Trust Scoring Factors**:

| Factor | Points | Reasoning |
|--------|--------|-----------|
| Session age | 0-100 | Older sessions more likely legitimate |
| Was previous primary | +50 | Proven trusted user |
| Observer mode | +20 | Already has video access |
| Queued mode | +10 | Actively waiting |
| Pending mode | +0 | Not yet trusted |
| Has nickname (when required) | +15 | Engaged user |
| Missing nickname (when required) | -30 | Incomplete setup |

**Example Scoring**:
```
Session A: age=2min, observer, nickname="Admin"
Score = 2 + 20 + 15 = 37

Session B: age=30min, was primary, observer, nickname="Bob"
Score = 30 + 50 + 20 + 15 = 115 ← SELECTED

Session C: age=1min, pending, no nickname
Score = 1 + 0 - 30 = -29
```

### Emergency Promotion Rate Limiting

**Purpose**: Prevent DoS attacks via rapid session churn

**Limits**:
- **Time-based**: 30 seconds between emergency promotions
- **Consecutive**: Maximum 3 consecutive emergency promotions
- **Bypass**: Both limits bypassed when NO primary exists

**Implementation**:
```go
isEmergencyPromotion := false
hasPrimary := sm.primarySessionID != ""

IF approvalRequired:
    isEmergencyPromotion = true

    IF !hasPrimary:
        LogError("CRITICAL: No primary - bypassing rate limits")
        // Promote immediately
    ELSE:
        IF now - lastEmergencyPromotion < 30 seconds:
            LogWarn("Emergency rate limit exceeded")
            RETURN  // Skip this promotion

        IF consecutiveEmergencyPromotions >= 3:
            LogError("Too many consecutive emergencies")
            RETURN  // Skip this promotion
```

**Counter Reset**:
- Consecutive counter resets on successful manual transfer
- Time limit is absolute (30 second window)

**Logging**:
All emergency promotions logged with:
- Reason (timeout, grace expiration, deadlock prevention)
- Selected session ID
- Trust score (if approval required)
- Whether rate limits were bypassed

### Emergency Promotion Testing

#### Test 1: No Primary Detection

1. Start with Session A as primary
2. Manually corrupt state: `sm.primarySessionID = ""`
3. Wait 10 seconds (validation runs)
4. Session A should be re-detected and set as primary

**Expected**: System auto-corrects invalid state.

#### Test 2: Primary Timeout

1. Session A becomes primary
2. Stop all activity (don't move mouse, don't type)
3. Wait 5 minutes
4. Session B should be promoted automatically
5. Session A should become observer

**Expected**: Clean timeout and promotion.

#### Test 3: Grace Period Expiration

1. Session A is primary, Session B is observer
2. Session A disconnects (network disconnect simulation)
3. Grace period starts (10 seconds)
4. Wait 15 seconds (let grace expire)
5. Session B promoted to primary

**Expected**: Session B takes over after grace expires.

#### Test 4: Rapid Disconnect Handling

1. Session A is primary, Sessions B and C are observers
2. Session A disconnects
3. Session B promoted (emergency)
4. IMMEDIATELY: Session B disconnects
5. Session C promoted (rate limit bypassed)

**Expected**: System maintains primary availability despite rapid disconnects.

#### Test 5: Emergency with Approval

1. Enable "Require Approval"
2. Session A is primary
3. Sessions B and C are pending (not approved)
4. Session A disconnects permanently
5. Grace expires
6. Session B OR C promoted (trust scoring selects highest)

**Expected**: System bypasses approval requirement to prevent deadlock.

---

## Permissions System

The permission system controls what actions each session mode can perform.

### Permission Model

**Architecture**: Role-Based Access Control (RBAC)

Each session mode has a predefined set of permissions that cannot be modified at runtime. This ensures consistent security boundaries.

### Permission Categories

#### Video & Display
- `video.view` - View video feed

#### Input Control
- `keyboard.input` - Send keyboard input
- `mouse.input` - Send mouse input
- `clipboard.paste` - Paste from clipboard

#### Session Management
- `session.transfer` - Transfer primary to another session
- `session.approve` - Approve pending sessions
- `session.kick` - Disconnect other sessions
- `session.request_primary` - Request primary control
- `session.release_primary` - Release primary control
- `session.manage` - Modify session settings

#### Power & Hardware
- `power.control` - ATX/DC power control
- `usb.control` - USB device management

#### Mount & Media
- `mount.media` - Mount virtual media
- `mount.unmedia` - Unmount virtual media
- `mount.list` - View mounted media

#### Extensions
- `extension.manage` - Enable/disable extensions
- `extension.atx` - ATX power extension
- `extension.dc` - DC power extension
- `extension.serial` - Serial console extension
- `extension.wol` - Wake-on-LAN extension

#### Terminal & Serial
- `terminal.access` - SSH terminal access
- `serial.access` - Serial console access

#### Settings
- `settings.read` - Read system settings
- `settings.write` - Modify system settings
- `settings.access` - Access settings UI

#### System Operations
- `system.reboot` - Reboot JetKVM
- `system.update` - Update firmware
- `system.network` - Network configuration

### Permissions by Mode

#### Primary Permissions

Primary sessions have **ALL** permissions:

```
✓ video.view
✓ keyboard.input
✓ mouse.input
✓ clipboard.paste
✓ session.transfer
✓ session.approve
✓ session.kick
✓ session.release_primary
✓ session.manage
✓ power.control
✓ usb.control
✓ mount.media
✓ mount.unmedia
✓ mount.list
✓ extension.* (all)
✓ terminal.access
✓ serial.access
✓ settings.* (all)
✓ system.* (all)

✗ session.request_primary (not needed)
```

#### Observer Permissions

Observer sessions have **limited** permissions:

```
✓ video.view
✓ session.request_primary
✓ mount.list (view only)

✗ All other permissions denied
```

#### Queued Permissions

Queued sessions have **same as observer**:

```
✓ video.view
✓ session.request_primary

✗ All other permissions denied
```

#### Pending Permissions

Pending sessions have **NO** permissions:

```
✗ video.view (no video access)
✗ ALL other permissions denied
```

### Permission Enforcement

**Check Timing**: Permissions are checked at multiple layers:

1. **RPC Method Call** - Before executing any JSON-RPC method
2. **UI Rendering** - Frontend hides unavailable controls
3. **WebRTC Channels** - Input channels only active for primary

**Enforcement Flow**:
```go
// RPC handler
func handleRPCMethod(session *Session, method string) {
    requiredPermission := GetMethodPermission(method)

    IF session does NOT have requiredPermission:
        RETURN "Permission denied: {permission}"

    // Execute method
}
```

**Permission Errors**:
```json
{
  "error": {
    "code": -32000,
    "message": "Permission denied: keyboard.input"
  }
}
```

### Method-to-Permission Mapping

**Power Control**:
```
setATXPowerAction → power.control
setDCPowerState   → power.control
setDCRestoreState → power.control
```

**USB Management**:
```
setUsbDeviceState → usb.control
setUsbDevices     → usb.control
```

**Mount Operations**:
```
mountUsb          → mount.media
unmountUsb        → mount.media
mountBuiltInImage → mount.media
getMassStorageMode → mount.list (read-only)
```

**Settings Operations**:
```
setNetworkSettings   → settings.write
setVideoFramerate    → settings.write
setSessionSettings   → session.manage
getNetworkSettings   → settings.read
```

**Session Operations**:
```
approveNewSession → session.approve
denyNewSession    → session.approve
transferSession   → session.transfer
requestPrimary    → session.request_primary
releasePrimary    → session.release_primary
```

**Input Operations**:
```
keyboardReport → keyboard.input
keypressReport → keyboard.input
absMouseReport → mouse.input
relMouseReport → mouse.input
```

*See full mapping in `/internal/session/permissions.go` lines 148-300*

### Permission Testing

#### Test 1: Observer Input Block

1. Session A is primary, Session B is observer
2. Session B attempts keyboard input via dev console:
   ```javascript
   ws.send(JSON.stringify({
     method: "keyboardReport",
     params: { keys: ["a"] }
   }))
   ```
3. Should receive: `Permission denied: keyboard.input`

**Expected**: Observer cannot send input.

#### Test 2: Observer Settings Block

1. Session B (observer) tries to access Settings page
2. Should see "Permission Denied" or redirect
3. Cannot view or modify any settings

**Expected**: Settings completely inaccessible to observers.

#### Test 3: Primary Full Access

1. Session A is primary
2. Verify can access:
   - All settings pages
   - Power controls
   - Mount/unmount media
   - Session management
   - Input controls

**Expected**: Primary has unrestricted access.

#### Test 4: Pending No Video

1. Enable "Require Approval"
2. Session B connects → Pending mode
3. Session B should see NO video feed
4. Session B cannot call `getVideoState` (permission denied)

**Expected**: Pending sessions completely blocked from video.

---

## Session Identification

Sessions are identified through multiple attributes for security, auditing, and user recognition.

### Session Identity Components

#### Session ID (UUID)

**Format**: UUID v4 (e.g., `f47ac10b-58cc-4372-a567-0e02b2c3d479`)

**Properties**:
- **Unique**: Globally unique identifier
- **Persistent**: Maintained across reconnections (within grace period)
- **Generated**: Server-side on first connection
- **Used For**: All internal session tracking and RPC references

**Reconnection**:
```go
IF session.ID exists in grace period map:
    Reconnect to existing session
ELSE:
    Create new session with new UUID
```

#### Source

**Values**: `"cloud"` or `"local"`

**Determination**:
- `cloud` - Connection via cloud relay (using cloud token)
- `local` - Direct local network connection

**Used For**:
- Session display in UI
- Trust scoring (local connections may be trusted more)
- Audit logging

**Visual**:
- Cloud sessions show cloud icon
- Local sessions show local network icon

#### Identity

**Format**: String (email, username, or IP address)

**Sources**:
- Cloud connections: User's email from OAuth
- Local connections: IP address or configured username

**Properties**:
- **Not Unique**: Multiple sessions can have same identity
- **Display**: Shown in session list
- **Validation**: Used to verify reconnection legitimacy

**Example**:
```
Cloud: "alice@example.com"
Local: "192.168.1.100"
```

#### Nickname

**Format**: 2-30 characters, alphanumeric with dashes/underscores

**Pattern**: `^[a-zA-Z0-9_-]+$`

**Sources**:
- **User-provided**: When "Require Nickname" enabled
- **Auto-generated**: When nickname not required

**Auto-Generation Format**:
```
u-{browser}-{short-id}

Examples:
u-chrome-a1b2
u-firefox-c3d4
u-safari-e5f6
u-edge-g7h8
u-user-i9j0  (unknown browser)
```

**Short ID**: Last 4 characters of session UUID (lowercase)

**Used For**:
- User-friendly identification
- Session list display
- Approval notifications
- Audit logs

**Validation**:
```go
IF len(nickname) < 2:
    ERROR "Nickname must be at least 2 characters"
IF len(nickname) > 30:
    ERROR "Nickname must be 30 characters or less"
IF !matches pattern:
    ERROR "Nickname can only contain letters, numbers, dashes, and underscores"
```

#### Browser Type

**Detected From**: User-Agent header

**Supported Browsers**:
- `chrome` - Chrome/Chromium
- `firefox` - Firefox
- `safari` - Safari
- `edge` - Edge
- `opera` - Opera
- `user` - Unknown/other

**Detection Logic**:
```go
ua := strings.ToLower(userAgent)

IF contains "edg/" OR "edge":
    RETURN "edge"
ELSE IF contains "firefox":
    RETURN "firefox"
ELSE IF contains "chrome":
    RETURN "chrome"
ELSE IF contains "safari" AND NOT "chrome":
    RETURN "safari"
ELSE IF contains "opera" OR "opr/":
    RETURN "opera"
ELSE:
    RETURN "user"
```

**Used For**:
- Auto-generating nicknames
- Browser-specific UI adjustments
- Debugging connection issues

#### Created At

**Type**: Timestamp (RFC 3339)

**Set**: On initial session creation

**Persistent**: Maintained across reconnections

**Used For**:
- Session age display
- Trust scoring (older = more trusted)
- Audit logs

#### Last Active

**Type**: Timestamp (RFC 3339)

**Updated On**:
- Mouse movement
- Keyboard input
- Any RPC method call
- WebRTC ping/pong

**Used For**:
- Primary timeout calculation
- Inactivity detection
- Session sorting (most recent first)

### Session Display in UI

**Session List Format**:

```
┌─────────────────────────────────────────────┐
│ 👤 u-chrome-a1b2          [PRIMARY]         │
│    alice@example.com                        │
│    Local • Active 2m ago                    │
│    ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━   │
│                                             │
│ 👤 u-firefox-c3d4         [OBSERVER]        │
│    bob@example.com                          │
│    Cloud • Active 5s ago                    │
│    [Request Control]                        │
│    ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━   │
│                                             │
│ 👤 TestUser               [QUEUED]          │
│    charlie@example.com                      │
│    Local • Active 1m ago                    │
│    Request Pending (#1 in queue)            │
│    [Approve] [Deny]                         │
│    ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━   │
│                                             │
│ 👤 (pending)              [PENDING]         │
│    david@example.com                        │
│    Cloud • Connected 30s ago                │
│    Waiting for approval                     │
│    [Approve] [Deny]                         │
└─────────────────────────────────────────────┘
```

**Display Elements**:
- **Nickname**: Large, prominent
- **Mode Badge**: Color-coded (Primary=green, Observer=blue, Queued=yellow, Pending=gray)
- **Identity**: Smaller, secondary text
- **Source Icon**: Cloud or local network icon
- **Activity**: Relative time ("2m ago", "just now")
- **Actions**: Context-specific buttons

### Identity Security

**Session Hijacking Prevention**:

When a session reconnects:
```go
IF existing.Identity != session.Identity:
    RETURN "Session ID already in use by different user"
IF existing.Source != session.Source:
    RETURN "Session ID already in use by different user"
```

**Reasoning**: Prevents malicious actors from stealing session IDs.

**Nickname Spoofing Prevention**:
- Nicknames validated server-side
- Cannot impersonate other sessions
- Auto-generated nicknames use session-specific data

---

## Testing Guide

This section provides comprehensive testing scenarios for all multi-session features.

### Test Environment Setup

**Prerequisites**:
1. JetKVM device (hardware or development environment)
2. Multiple browsers (Chrome, Firefox, Safari) for multi-session testing
3. Network access (local and/or cloud)
4. Admin access to session settings

**Recommended Setup**:
```
Browser 1 (Chrome):   Primary session
Browser 2 (Firefox):  Observer session
Browser 3 (Safari):   Test session
```

**Configuration Reset**:
Before each test suite, reset to defaults:
```json
{
  "requireApproval": false,
  "requireNickname": false,
  "reconnectGrace": 10,
  "primaryTimeout": 300,
  "privateKeystrokes": false,
  "maxRejectionAttempts": 3
}
```

### Core Functionality Tests

#### TEST-001: Basic Multi-Session

**Objective**: Verify multiple sessions can connect simultaneously

**Steps**:
1. Connect with Browser 1 → Verify becomes primary
2. Connect with Browser 2 → Verify becomes observer
3. Connect with Browser 3 → Verify becomes observer
4. Check session list shows all 3 sessions
5. Verify Browser 1 can control, Browsers 2&3 can only view

**Expected**:
- ✓ 3 sessions visible in session list
- ✓ 1 primary, 2 observers
- ✓ Only primary can send input
- ✓ All sessions see video feed

**Pass Criteria**: All checkmarks verified

---

#### TEST-002: Primary Transfer

**Objective**: Verify primary role can be transferred between sessions

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Browser 1 clicks "Transfer Control" next to Browser 2
3. Verify Browser 2 becomes primary
4. Verify Browser 1 becomes observer
5. Verify Browser 2 can now send input
6. Verify Browser 1 cannot send input

**Expected**:
- ✓ Smooth transition of primary role
- ✓ UI updates immediately
- ✓ Input control switches correctly
- ✓ No video interruption

**Pass Criteria**: All checkmarks verified

---

#### TEST-003: Request and Approve Control

**Objective**: Verify observers can request and receive control

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Browser 2 clicks "Request Control"
3. Verify Browser 2 enters queued state
4. Verify Browser 1 sees approval notification
5. Browser 1 clicks "Approve"
6. Verify Browser 2 becomes primary
7. Verify Browser 1 becomes observer

**Expected**:
- ✓ Request notification appears for primary
- ✓ Queued state displayed correctly
- ✓ Approval transfers control smoothly

**Pass Criteria**: All checkmarks verified

---

#### TEST-004: Request and Deny Control

**Objective**: Verify primary can deny control requests

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Browser 2 clicks "Request Control"
3. Browser 1 clicks "Deny"
4. Verify Browser 2 returns to observer state
5. Verify Browser 1 remains primary
6. Verify Browser 2 can request again

**Expected**:
- ✓ Denial returns session to observer
- ✓ No disruption to primary
- ✓ Can request multiple times (until limit)

**Pass Criteria**: All checkmarks verified

---

#### TEST-005: Release Control

**Objective**: Verify primary can voluntarily release control

**Steps**:
1. Browser 1 is primary, Browsers 2&3 are observers
2. Browser 1 clicks "Release Control"
3. Verify Browser 1 becomes observer
4. Verify Browser 2 OR Browser 3 becomes primary (system selects)
5. Verify new primary can send input

**Expected**:
- ✓ Primary releases control successfully
- ✓ Observer auto-promoted
- ✓ All sessions functioning correctly

**Pass Criteria**: All checkmarks verified

---

### Grace Period Tests

#### TEST-006: Grace Period Reconnection (Success)

**Objective**: Verify primary can reconnect within grace period

**Steps**:
1. Browser 1 is primary
2. Close Browser 1 tab (simulating accidental close)
3. Wait 3 seconds (within 10-second grace)
4. Reopen Browser 1 (same session ID via browser history)
5. Verify reconnects as primary

**Expected**:
- ✓ Session reconnects successfully
- ✓ Primary status restored
- ✓ No interruption to other sessions

**Pass Criteria**: All checkmarks verified

---

#### TEST-007: Grace Period Expiration

**Objective**: Verify grace period expires and promotes observer

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Close Browser 1 tab
3. Wait 15 seconds (exceeds 10-second grace)
4. Verify Browser 2 becomes primary
5. Reopen Browser 1
6. Verify Browser 1 reconnects as observer

**Expected**:
- ✓ Browser 2 promoted after grace expires
- ✓ Browser 1 returns as observer
- ✓ No double-primary state

**Pass Criteria**: All checkmarks verified

---

#### TEST-008: Intentional Logout (No Grace Period)

**Objective**: Verify logout bypasses grace period

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Browser 1 clicks "Logout" button
3. Verify Browser 2 immediately becomes primary (no 10-second wait)
4. Reopen Browser 1
5. Verify Browser 1 enters as new session (observer)

**Expected**:
- ✓ Immediate promotion on logout
- ✓ No grace period applied
- ✓ Clean session cleanup

**Pass Criteria**: All checkmarks verified

---

### Approval System Tests

#### TEST-009: Basic Approval Flow

**Objective**: Verify approval system gates new connections

**Steps**:
1. Enable "Require Approval"
2. Browser 1 connects → Becomes primary
3. Browser 2 connects → Becomes pending
4. Verify Browser 2 sees "Waiting for approval" (no video)
5. Verify Browser 1 sees approval notification
6. Browser 1 clicks "Approve"
7. Verify Browser 2 becomes observer with video access

**Expected**:
- ✓ Pending session has no video access
- ✓ Approval notification displayed
- ✓ Video access granted after approval

**Pass Criteria**: All checkmarks verified

---

#### TEST-010: Approval with Nickname Required

**Objective**: Verify nickname requirement gates approval

**Steps**:
1. Enable "Require Approval" and "Require Nickname"
2. Browser 1 connects → Becomes primary
3. Browser 2 connects (no nickname provided) → Becomes pending
4. Verify Browser 1 does NOT see approval notification yet
5. Browser 2 enters nickname "TestUser"
6. Verify Browser 1 NOW sees approval notification
7. Browser 1 approves
8. Verify Browser 2 becomes observer

**Expected**:
- ✓ Approval delayed until nickname provided
- ✓ Nickname requirement enforced
- ✓ Approval proceeds after nickname entered

**Pass Criteria**: All checkmarks verified

---

#### TEST-011: Approval Denial and Rejection Limit

**Objective**: Verify rejection limit blocks spam

**Steps**:
1. Enable "Require Approval", set max rejections = 3
2. Browser 1 is primary
3. Browser 2 connects → Pending
4. Browser 1 denies → Browser 2 disconnected (count=1)
5. Browser 2 reconnects → Pending
6. Browser 1 denies → Browser 2 disconnected (count=2)
7. Browser 2 reconnects → Pending
8. Browser 1 denies → Browser 2 blocked (count=3)
9. Browser 2 tries to reconnect → Connection rejected

**Expected**:
- ✓ Rejection counter increments
- ✓ After 3 rejections, session auto-blocked
- ✓ Cannot reconnect after blocking

**Pass Criteria**: All checkmarks verified

---

#### TEST-012: Emergency Approval Bypass

**Objective**: Verify pending session promoted if primary leaves

**Steps**:
1. Enable "Require Approval"
2. Browser 1 is primary
3. Browser 2 connects → Pending (no video)
4. Browser 1 disconnects permanently (close browser)
5. Wait 15 seconds (grace period expires)
6. Verify Browser 2 promoted to primary (approval bypassed)
7. Verify Browser 2 now has video access and full control

**Expected**:
- ✓ Pending session promoted to prevent deadlock
- ✓ Emergency bypass logged
- ✓ System maintains availability

**Pass Criteria**: All checkmarks verified

---

### Timeout Tests

#### TEST-013: Primary Inactivity Timeout

**Objective**: Verify inactive primary is demoted

**Steps**:
1. Set primary timeout = 60 seconds (for faster testing)
2. Browser 1 is primary, Browser 2 is observer
3. Browser 1: Do NOT move mouse or type for 60 seconds
4. After 60 seconds, verify Browser 1 demoted to observer
5. Verify Browser 2 promoted to primary

**Expected**:
- ✓ Timeout triggers at configured interval
- ✓ Inactive session demoted
- ✓ Observer auto-promoted

**Pass Criteria**: All checkmarks verified

---

#### TEST-014: Timeout with Activity

**Objective**: Verify activity resets timeout timer

**Steps**:
1. Set primary timeout = 60 seconds
2. Browser 1 is primary
3. Wait 50 seconds
4. Browser 1: Move mouse (reset timer)
5. Wait another 50 seconds
6. Browser 1: Type something (reset timer)
7. Wait 50 seconds
8. Verify Browser 1 still primary (timeout keeps resetting)

**Expected**:
- ✓ Activity prevents timeout
- ✓ Timer resets on each action
- ✓ Primary retained indefinitely with activity

**Pass Criteria**: All checkmarks verified

---

### Permission Tests

#### TEST-015: Observer Input Block

**Objective**: Verify observers cannot send input

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Browser 2: Try clicking on video feed
3. Browser 2: Try typing
4. Browser 2: Open browser console, try:
   ```javascript
   // Send keyboard input via RPC
   rpc.call("keyboardReport", {keys: ["a"]})
   ```
5. Verify all input attempts blocked
6. Verify error: "Permission denied: keyboard.input"

**Expected**:
- ✓ UI input ignored for observers
- ✓ RPC calls return permission error
- ✓ No input reaches device

**Pass Criteria**: All checkmarks verified

---

#### TEST-016: Observer Settings Block

**Objective**: Verify observers cannot access settings

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Browser 2: Try to navigate to Settings page
3. Verify redirected or blocked
4. Browser 2: Open console, try:
   ```javascript
   rpc.call("setNetworkSettings", {dhcp: false})
   ```
5. Verify error: "Permission denied: settings.write"

**Expected**:
- ✓ Settings page inaccessible
- ✓ Settings RPC calls blocked
- ✓ No configuration changes possible

**Pass Criteria**: All checkmarks verified

---

#### TEST-017: Pending Video Block

**Objective**: Verify pending sessions have no video access

**Steps**:
1. Enable "Require Approval"
2. Browser 1 is primary
3. Browser 2 connects → Pending
4. Verify Browser 2 shows "Waiting for approval" message
5. Verify Browser 2 has NO video feed
6. Browser 2: Open console, try:
   ```javascript
   rpc.call("getVideoState", {})
   ```
7. Verify error: "Permission denied: video.view"

**Expected**:
- ✓ No video feed visible
- ✓ Video RPC calls blocked
- ✓ Complete video blackout until approved

**Pass Criteria**: All checkmarks verified

---

### Edge Case Tests

#### TEST-018: Multiple Rapid Disconnects

**Objective**: Verify system maintains primary during rapid churn

**Steps**:
1. Browser 1 is primary, Browsers 2&3 are observers
2. Close Browser 1 → Browser 2 promoted
3. IMMEDIATELY close Browser 2 (within 1 second) → Browser 3 promoted
4. Verify Browser 3 is now primary
5. Verify rate limiting did NOT block promotion

**Expected**:
- ✓ System always maintains a primary
- ✓ Rate limits bypassed when necessary
- ✓ No deadlock state

**Pass Criteria**: All checkmarks verified

---

#### TEST-019: Transfer with Immediate Refresh

**Objective**: Verify transferred session retains primary on refresh

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Browser 1 transfers to Browser 2
3. Browser 2 becomes primary
4. IMMEDIATELY: Browser 2 refresh page (F5)
5. Verify Browser 2 reconnects as primary
6. Verify Browser 1 remains observer

**Expected**:
- ✓ Refresh does not lose primary status
- ✓ No reversion to previous primary
- ✓ Transfer is permanent

**Pass Criteria**: All checkmarks verified

---

#### TEST-020: Blacklist Expiration

**Objective**: Verify transfer blacklist expires after 60 seconds

**Steps**:
1. Browser 1 is primary, Browser 2 is observer
2. Browser 1 transfers to Browser 2
3. Browser 1 now observer and blacklisted
4. Browser 1 tries to request control → Blocked (blacklisted)
5. Wait 60 seconds
6. Browser 1 tries to request control → Allowed
7. Browser 2 approves
8. Verify Browser 1 becomes primary

**Expected**:
- ✓ Blacklist blocks immediate re-takeover
- ✓ Blacklist expires after 60 seconds
- ✓ Normal flow resumes after expiration

**Pass Criteria**: All checkmarks verified

---

### Stress Tests

#### TEST-021: Maximum Session Limit

**Objective**: Verify maximum session limit enforced

**Steps**:
1. Connect 10 browsers (maximum)
2. Verify all 10 sessions accepted
3. Attempt to connect 11th browser
4. Verify connection rejected with "Maximum sessions reached"

**Expected**:
- ✓ Exactly 10 sessions allowed
- ✓ 11th connection rejected
- ✓ Existing sessions unaffected

**Pass Criteria**: All checkmarks verified

---

#### TEST-022: Rapid Connect/Disconnect

**Objective**: Verify system stable under rapid session churn

**Steps**:
1. Script or manually: Connect and disconnect 20 sessions rapidly
2. Monitor system logs for errors
3. Verify session manager remains stable
4. Verify at least one session becomes primary

**Expected**:
- ✓ No crashes or deadlocks
- ✓ System maintains consistency
- ✓ Clean session cleanup

**Pass Criteria**: All checkmarks verified

---

#### TEST-023: Grace Period DoS Protection

**Objective**: Verify grace period limit prevents memory exhaustion

**Steps**:
1. Connect 15 sessions
2. Disconnect all 15 sessions rapidly (create 15 grace periods)
3. Verify only 10 grace periods maintained (oldest evicted)
4. Monitor memory usage (should not spike)

**Expected**:
- ✓ Grace period limit enforced
- ✓ Oldest entries evicted
- ✓ No memory exhaustion

**Pass Criteria**: All checkmarks verified

---

### Session Settings Tests

#### TEST-024: Private Keystrokes

**Objective**: Verify keystroke privacy setting works

**Steps**:
1. Enable "Private Keystrokes"
2. Browser 1 is primary, Browser 2 is observer
3. Browser 1: Type "test123"
4. Browser 2: Verify NO keystroke events received
5. Disable "Private Keystrokes"
6. Browser 1: Type "test123"
7. Browser 2: Verify keystroke events received (for awareness)

**Expected**:
- ✓ Private mode blocks keystroke visibility
- ✓ Non-private mode shows keystrokes
- ✓ Mouse events always visible

**Pass Criteria**: All checkmarks verified

---

#### TEST-025: Configurable Grace Period

**Objective**: Verify grace period duration is configurable

**Steps**:
1. Set grace period = 30 seconds
2. Browser 1 is primary
3. Close Browser 1
4. Wait 15 seconds
5. Reopen Browser 1 → Should reconnect as primary
6. Close Browser 1 again
7. Wait 35 seconds
8. Reopen Browser 1 → Should reconnect as observer

**Expected**:
- ✓ Grace period respects configured duration
- ✓ Reconnection successful within window
- ✓ Promotion occurs after expiration

**Pass Criteria**: All checkmarks verified

---

### Regression Tests

#### TEST-026: No Double Primary

**Objective**: Verify system prevents multiple primary sessions

**Steps**:
1. Connect multiple sessions
2. Perform various transfers and promotions
3. After each action, verify exactly 1 primary exists
4. Check system logs for "Multiple primary sessions detected"

**Expected**:
- ✓ Always exactly 1 primary
- ✓ No double-primary state
- ✓ Automatic correction if detected

**Pass Criteria**: All checkmarks verified

---

#### TEST-027: Orphaned Primary Cleanup

**Objective**: Verify orphaned primary IDs are cleaned up

**Steps**:
1. Browser 1 is primary
2. Simulate crash: Kill browser without proper disconnect
3. Wait for system to detect disconnect
4. Verify primary slot cleared
5. Verify observer promoted
6. Check no orphaned primary ID remains

**Expected**:
- ✓ Orphaned primary detected
- ✓ Automatic cleanup
- ✓ New primary promoted

**Pass Criteria**: All checkmarks verified

---

### Performance Tests

#### TEST-028: Session List Update Latency

**Objective**: Verify session list updates are timely

**Steps**:
1. Have 5 sessions connected
2. Transfer primary from Browser 1 to Browser 2
3. Measure time for session list to update on all browsers
4. Verify update within 500ms

**Expected**:
- ✓ Updates received within 500ms
- ✓ All sessions updated simultaneously
- ✓ No stale UI states

**Pass Criteria**: All checkmarks verified

---

#### TEST-029: Broadcast Throttling

**Objective**: Verify broadcast throttling prevents spam

**Steps**:
1. Rapidly transfer primary 10 times in 1 second
2. Monitor session list update events
3. Verify updates throttled (not 10 updates)
4. Verify final state is correct

**Expected**:
- ✓ Broadcasts throttled to prevent spam
- ✓ Final state accurate
- ✓ No performance degradation

**Pass Criteria**: All checkmarks verified

---

### Test Results Template

For each test, record results in this format:

```
TEST-XXX: [Test Name]
Date: YYYY-MM-DD
Tester: [Name]
Device: JetKVM [Model]
Firmware: [Version]

Result: PASS / FAIL / BLOCKED

Notes:
- [Any observations]
- [Deviations from expected behavior]
- [Performance metrics if applicable]

Issues Found:
- [Issue #1 description]
- [Issue #2 description]
```

---

## Troubleshooting

### Common Issues

#### Issue: Session stuck in Pending mode

**Symptoms**: Session shows "Waiting for approval" indefinitely

**Causes**:
- Primary session disconnected before approving
- Pending session timeout (1 minute) not expired yet

**Solutions**:
1. Wait for pending timeout (1 minute)
2. If primary exists, manually approve/deny
3. If no primary, system will auto-promote after grace period

---

#### Issue: Cannot send input as primary

**Symptoms**: Primary session cannot control mouse/keyboard

**Causes**:
- WebRTC HID channel not established
- Permission error
- Browser compatibility issue

**Solutions**:
1. Refresh browser (F5)
2. Check browser console for errors
3. Verify primary badge is visible
4. Try different browser (Chrome recommended)

---

#### Issue: Video not visible for observer

**Symptoms**: Observer sees black screen

**Causes**:
- Permission issue (if pending)
- WebRTC connection failure
- Video feed not active

**Solutions**:
1. Verify session mode is Observer (not Pending)
2. Check WebRTC connection status
3. Verify HDMI input is active
4. Refresh browser

---

#### Issue: Grace period not working

**Symptoms**: Primary loses control on reconnect

**Causes**:
- Manual transfer occurred (cleared grace)
- Grace period expired
- Session blacklisted

**Solutions**:
1. Check grace period duration setting
2. Verify reconnection within grace window
3. Check if session was manually transferred (60s blacklist)
4. Review logs for grace period events

---

#### Issue: Emergency promotion selecting wrong session

**Symptoms**: Unexpected session becomes primary

**Causes**:
- Trust scoring selected different session
- Blacklist blocking preferred session

**Solutions**:
1. Review trust score logs
2. Check blacklist status
3. Verify session age and properties
4. Use manual transfer for explicit control

---

## Configuration Reference

### Default Values

```json
{
  "multi_session": {
    "enabled": true,
    "max_sessions": 10,
    "primary_timeout_seconds": 300,
    "allow_cloud_override": true,
    "require_auth_transfer": false
  },
  "session_settings": {
    "requireApproval": false,
    "requireNickname": false,
    "reconnectGrace": 10,
    "primaryTimeout": 300,
    "privateKeystrokes": false,
    "maxRejectionAttempts": 3
  }
}
```

### Recommended Configurations

#### Secure Environment

```json
{
  "session_settings": {
    "requireApproval": true,
    "requireNickname": true,
    "reconnectGrace": 10,
    "primaryTimeout": 300,
    "privateKeystrokes": true,
    "maxRejectionAttempts": 3
  }
}
```

**Use Case**: High-security environments, data centers, production systems

---

#### Collaborative Environment

```json
{
  "session_settings": {
    "requireApproval": false,
    "requireNickname": false,
    "reconnectGrace": 30,
    "primaryTimeout": 600,
    "privateKeystrokes": false,
    "maxRejectionAttempts": 5
  }
}
```

**Use Case**: Team collaboration, pair programming, training

---

#### Personal Use

```json
{
  "session_settings": {
    "requireApproval": false,
    "requireNickname": false,
    "reconnectGrace": 10,
    "primaryTimeout": 0,
    "privateKeystrokes": false,
    "maxRejectionAttempts": 3
  }
}
```

**Use Case**: Single user accessing from multiple devices

---

## Appendix

### Session State Diagram

```
[New Connection]
       ↓
   ┌───────────────────────────────────────┐
   │   Is there a primary?                 │
   └───────────────────────────────────────┘
          NO ↓         ↓ YES
       [Primary]   ┌──────────────────┐
                   │ Approval needed? │
                   └──────────────────┘
                      NO ↓    ↓ YES
                 [Observer] [Pending]
                      ↓
            ┌─────────────────┐
            │ Request Control │
            └─────────────────┘
                      ↓
                  [Queued]
                      ↓
            ┌─────────────────┐
            │ Approved/Denied │
            └─────────────────┘
                  ↓      ↓
            [Primary] [Observer]
```

### Glossary

**Session**: A WebRTC connection from a browser to the JetKVM device

**Primary**: The session with exclusive control over input and settings

**Observer**: A session that can view video but cannot control anything

**Queued**: A session that has requested control and is waiting for approval

**Pending**: A session waiting for approval to even view video

**Grace Period**: Time window for reconnection without losing session state

**Blacklist**: Temporary block preventing a session from becoming primary

**Transfer**: Changing primary status from one session to another

**Emergency Promotion**: Automatic promotion when no primary exists

**Trust Score**: Calculated value determining promotion priority

---

### Changelog

**Version 1.0** (Initial Release)
- Multi-session support with 4 session modes
- Role-based permissions system
- Grace period reconnection
- Session approval workflow
- Emergency promotion system
- Transfer protection (blacklisting)
- Automatic nickname generation
- Trust-based session selection
- Configurable timeouts and limits

---

**Document Version**: 1.0
**Last Updated**: 2025
**Maintained By**: JetKVM Development Team
