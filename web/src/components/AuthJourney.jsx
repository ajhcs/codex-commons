import Branch from "../icons/Branch.tsx";

const JOURNEY_STEPS = ["Connect", "Authorize", "Enter Commons"];

function currentStep(stage) {
  if (stage === "authorize") return 1;
  if (stage === "profile") return 2;
  if (stage === "complete") return 3;
  return 0;
}

function stepState(index, activeStep) {
  if (activeStep === 3 || index < activeStep) return "complete";
  if (index === activeStep) return "current";
  return "upcoming";
}

function announcement(stage, identity) {
  if (stage === "connecting") return "Connecting Commons to the Codex App Server.";
  if (stage === "authorize") return "Connected to Codex. Authorization is waiting for you.";
  if (stage === "profile") return "Codex is authorized. Choose the identity you will use in Commons.";
  if (stage === "complete") return `Connected as ${identity?.displayName || "your Commons identity"}${identity?.handle ? `, at ${identity.handle}` : ""}.`;
  return "Ready to connect Codex to Commons.";
}

export default function AuthJourney({ stage = "ready", identity = null }) {
  const activeStep = currentStep(stage);
  const displayName = identity?.displayName?.trim() || (stage === "profile" ? "Your Commons identity" : "Commons");
  const handle = identity?.handle?.trim() ? `@${identity.handle.trim().replace(/^@/, "")}` : (stage === "profile" ? "Choose your handle" : "Durable project memory");

  return (
    <section className={`auth-journey auth-journey--${stage}`} aria-label="Codex connection">
      <p className="sr-only" role="status" aria-live="polite">{announcement(stage, identity)}</p>
      <div className="auth-connection-scene" aria-hidden="true">
        <div className="auth-connection-endpoint auth-connection-endpoint--codex">
          <span><strong>Codex</strong><small>App Server</small></span>
        </div>
        <div className="auth-thread" data-stage={stage}>
          <span className="auth-thread-progress" />
          <span className="auth-thread-signal" />
        </div>
        <div className="auth-connection-endpoint auth-connection-endpoint--commons">
          <span className="auth-endpoint-glyph"><Branch /></span>
          <span><strong>{displayName}</strong><small>{handle}</small></span>
        </div>
      </div>
      <ol className="auth-journey-steps" aria-label="Sign-in progress">
        {JOURNEY_STEPS.map((label, index) => {
          const state = stepState(index, activeStep);
          return <li key={label} data-state={state} aria-current={state === "current" ? "step" : undefined}><span aria-hidden="true" /><strong>{label}</strong></li>;
        })}
      </ol>
    </section>
  );
}
