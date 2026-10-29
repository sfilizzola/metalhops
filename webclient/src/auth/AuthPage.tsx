import { useEffect, useState } from "react";
import type { FormEvent } from "react";
import { Link } from "react-router-dom";
import { supabase } from "../supabase";
import { Alert } from "../ui/Alert";
import { Button } from "../ui/Button";
import { Field } from "../ui/Field";
import "./auth.css";

const LOGIN_ERROR = "Invalid email or password";
const SIGNUP_ERROR = "Could not create the account. Check your details and try again.";

// Login (/login) and sign-up (/signup) share one card; errors stay generic so they never reveal whether an email is registered.
export function AuthPage({ mode }: { mode: "in" | "up" }) {
  const signUp = mode === "up";
  const title = signUp ? "Sign up" : "Sign in";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");

  useEffect(() => {
    document.title = `${title} · MetalHops`;
    setError("");
    setNotice("");
  }, [title]);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setNotice("");
    if (!email.trim() || (signUp && password.length < 8)) {
      setError(signUp ? "Enter an email and a password of at least 8 characters." : LOGIN_ERROR);
      return;
    }
    setPending(true);
    try {
      const { data, error: err } = signUp
        ? await supabase!.auth.signUp({ email: email.trim(), password })
        : await supabase!.auth.signInWithPassword({ email: email.trim(), password });
      if (err) setError(signUp ? SIGNUP_ERROR : LOGIN_ERROR);
      else if (signUp && !data.session) setNotice("Check your email to confirm your account");
    } catch {
      setError(signUp ? SIGNUP_ERROR : LOGIN_ERROR);
    } finally {
      setPending(false);
    }
  }

  return (
    <main className="auth">
      <form className="auth__card" onSubmit={submit} noValidate>
        <p className="t-title auth__wordmark">MetalHops</p>
        <h1 className="t-heading">{title}</h1>
        <Field id="auth-email" label="Email" type="email" autoComplete="email" value={email} onChange={(e) => setEmail(e.target.value)} />
        <Field
          id="auth-password"
          label="Password"
          type="password"
          autoComplete={signUp ? "new-password" : "current-password"}
          help={signUp ? "At least 8 characters." : undefined}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        {error && <Alert kind="error">{error}</Alert>}
        {notice && <Alert kind="success">{notice}</Alert>}
        <Button type="submit" size="lg" loading={pending}>{title}</Button>
        <Link className="btn btn--ghost auth__switch" to={signUp ? "/login" : "/signup"}>
          {signUp ? "Have an account? Sign in" : "Need an account? Sign up"}
        </Link>
      </form>
    </main>
  );
}
