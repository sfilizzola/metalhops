import { useCallback, useEffect, useState } from "react";
import type { FormEvent } from "react";
import type { Session } from "@supabase/supabase-js";
import { getMe, saveProfile } from "./api";
import type { ApiFailure, GetToken, MeResult, Profile } from "./api";
import { apiBaseUrl, supabase } from "./supabase";
import "./App.css";

const USERNAME_RE = /^[A-Za-z0-9_]{3,30}$/;

function Failure({ failure, onRetry }: { failure: ApiFailure; onRetry: () => void }) {
  if (failure.kind === "forbidden") return <p role="alert">This account can't use the app.</p>;
  const requestId = "requestId" in failure ? failure.requestId : undefined;
  return (
    <div role="alert">
      <p>Something went wrong. Please try again.{requestId ? ` (request ${requestId})` : ""}</p>
      <button type="button" onClick={onRetry}>Retry</button>
    </div>
  );
}

function AuthForm() {
  const [mode, setMode] = useState<"in" | "up">("in");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setNotice("");
    const signUp = mode === "up";
    if (!email.trim() || (signUp && password.length < 8)) {
      setError(signUp ? "Enter an email and a password of at least 8 characters." : "Invalid email or password");
      return;
    }
    setPending(true);
    const { data, error: err } = signUp
      ? await supabase!.auth.signUp({ email: email.trim(), password })
      : await supabase!.auth.signInWithPassword({ email: email.trim(), password });
    setPending(false);
    if (err) setError(signUp ? "Could not create the account. Check your details and try again." : "Invalid email or password");
    else if (signUp && !data.session) setNotice("Check your email to confirm your account");
  }

  return (
    <form onSubmit={submit}>
      <h1>{mode === "in" ? "Sign in" : "Sign up"}</h1>
      <label>Email<input type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} /></label>
      <label>Password<input type="password" autoComplete={mode === "in" ? "current-password" : "new-password"} value={password} onChange={(e) => setPassword(e.target.value)} /></label>
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      <button type="submit" disabled={pending}>{mode === "in" ? "Sign in" : "Sign up"}</button>
      <button type="button" onClick={() => { setMode(mode === "in" ? "up" : "in"); setError(""); setNotice(""); }}>
        {mode === "in" ? "Need an account? Sign up" : "Have an account? Sign in"}
      </button>
    </form>
  );
}

function ProfileForm({ getToken, onSaved, onFailure }: { getToken: GetToken; onSaved: (p: Profile) => void; onFailure: (f: ApiFailure) => void }) {
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [general, setGeneral] = useState("");
  const [pending, setPending] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setGeneral("");
    const name = displayName.trim();
    const next: Record<string, string> = {};
    if (!USERNAME_RE.test(username)) next.username = "Username must be 3-30 letters, numbers or underscores";
    if (name.length < 1 || name.length > 80) next.display_name = "Display name must be 1-80 characters";
    setErrors(next);
    if (Object.keys(next).length) return;

    setPending(true);
    const res = await saveProfile(apiBaseUrl, getToken, { username, display_name: name });
    setPending(false);
    switch (res.kind) {
      case "ok": return onSaved(res.profile);
      case "invalid":
        if (res.fields && Object.keys(res.fields).length) setErrors(res.fields);
        else setGeneral(res.code === "username_reserved" ? "That username is reserved" : "Check the details and try again");
        return;
      case "username_taken": return setErrors({ username: "That username is taken" });
      case "suspended": return setGeneral("This profile is suspended");
      default: return onFailure(res);
    }
  }

  return (
    <form onSubmit={submit}>
      <label>Username<input value={username} onChange={(e) => setUsername(e.target.value)} />
        {errors.username && <span role="alert">{errors.username}</span>}</label>
      <label>Display name<input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
        {errors.display_name && <span role="alert">{errors.display_name}</span>}</label>
      {general && <p role="alert">{general}</p>}
      <button type="submit" disabled={pending}>Create profile</button>
    </form>
  );
}

function Home({ email, userId }: { email: string; userId: string }) {
  const [me, setMe] = useState<MeResult | null>(null);
  const [slow, setSlow] = useState(false);
  const [attempt, setAttempt] = useState(0);

  // Read fresh on every call; the token is never kept in state.
  const getToken = useCallback<GetToken>(async () => (await supabase!.auth.getSession()).data.session?.access_token ?? null, []);

  const handle = useCallback((r: MeResult | ApiFailure) => {
    if (r.kind === "unauthorized") void supabase!.auth.signOut({ scope: "local" });
    else setMe(r as MeResult);
  }, []);

  useEffect(() => {
    const ctl = new AbortController();
    setMe(null);
    setSlow(false);
    const timer = setTimeout(() => setSlow(true), 5000);
    getMe(apiBaseUrl, getToken, ctl.signal).then((r) => {
      if (!ctl.signal.aborted) handle(r);
    }).finally(() => clearTimeout(timer));
    return () => { ctl.abort(); clearTimeout(timer); };
  }, [userId, attempt, getToken, handle]);

  return (
    <>
      <p>Signed in as {email}</p>
      {!me && <p role="status">{slow ? "Waking up the server…" : "Loading…"}</p>}
      {me?.kind === "ok" && (
        <section>
          <h1>{me.profile.display_name}</h1>
          <p>@{me.profile.username}</p>
          <p>Status: {me.profile.status}</p>
        </section>
      )}
      {me?.kind === "no_profile" && (
        <section>
          <p>Signed in — no profile yet</p>
          <ProfileForm getToken={getToken} onSaved={(profile) => setMe({ kind: "ok", profile })} onFailure={handle} />
        </section>
      )}
      {me && me.kind !== "ok" && me.kind !== "no_profile" && <Failure failure={me} onRetry={() => setAttempt((n) => n + 1)} />}
      <button type="button" onClick={() => void supabase!.auth.signOut()}>Log out</button>
    </>
  );
}

export default function App() {
  const [session, setSession] = useState<Session | null | undefined>(undefined);

  useEffect(() => {
    if (!supabase) return;
    const { data } = supabase.auth.onAuthStateChange((_event, s) => setSession(s));
    return () => data.subscription.unsubscribe();
  }, []);

  if (!supabase) {
    return <main><p role="alert">Missing configuration: set VITE_SUPABASE_URL and VITE_SUPABASE_PUBLISHABLE_KEY.</p></main>;
  }
  if (session === undefined) return <main><p role="status">Loading…</p></main>;
  return (
    <main>
      {session ? <Home email={session.user.email ?? ""} userId={session.user.id} /> : <AuthForm />}
    </main>
  );
}
