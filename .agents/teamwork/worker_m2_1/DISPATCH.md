## 2026-10-29T03:29:32Z
You are Worker M2 (Implementation of R2 - Rate Controller with Human Pauses and Deterministic Jitter) for BuyaCAR Omnicanal.
Your working directory is: c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\worker_m2_1
Read your DISPATCH.md, c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\.agents\teamwork\ORIGINAL_REQUEST.md, c:\Users\Buyac\OneDrive\Documentos\I&D\buyacar-omnicanal-agent-workspace\PROJECT.md, and the design specification in .agents/teamwork/explorer_survey_channel_1/analysis.md.
Implement RateController and comprehensive tests in app_build/internal/channel/:
1. Enforce strictly max 120 msgs/hr and max 250 msgs/day per channel.
2. Calculate deterministic human pauses: 3,200 ms chats, 2,500 ms channels with +/-30% jitter ([2240ms, 4160ms] and [1750ms, 3250ms]).
3. Provide virtual time parameterization (accepting `now time.Time`) and context-cancellable waits.
4. Ensure thread-safety (sync.RWMutex), 0 race conditions, and 100% test pass (`go test -v ./...`).
Deliver handoff.md in your working directory and notify caller via send_message.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.
