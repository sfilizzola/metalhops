import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { getMe } from "../api";
import type { GetToken, MeResult, Profile } from "../api";
import { apiBaseUrl, supabase } from "../supabase";

type MeContextValue = {
  me: MeResult | null;
  slow: boolean;
  reload: () => void;
  setProfile: (profile: Profile) => void;
  getToken: GetToken;
};

const MeContext = createContext<MeContextValue | null>(null);

// Read fresh on every call; the token is never kept in state.
const getToken: GetToken = async () => (await supabase!.auth.getSession()).data.session?.access_token ?? null;

export function MeProvider({ userId, children }: { userId: string; children: ReactNode }) {
  const [me, setMe] = useState<MeResult | null>(null);
  const [slow, setSlow] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const ctl = new AbortController();
    setMe(null);
    setSlow(false);
    const timer = setTimeout(() => setSlow(true), 5000);
    getMe(apiBaseUrl, getToken, ctl.signal).then((r) => {
      if (ctl.signal.aborted) return;
      if (r.kind === "unauthorized") void supabase!.auth.signOut({ scope: "local" });
      else setMe(r);
    }).catch(() => {
      // e.g. getSession throwing; without this the page stays on "Loading…".
      if (!ctl.signal.aborted) setMe({ kind: "error" });
    }).finally(() => clearTimeout(timer));
    return () => { ctl.abort(); clearTimeout(timer); };
  }, [userId, attempt]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const setProfile = useCallback((profile: Profile) => setMe({ kind: "ok", profile }), []);
  const value = useMemo(() => ({ me, slow, reload, setProfile, getToken }), [me, slow, reload, setProfile]);
  return <MeContext.Provider value={value}>{children}</MeContext.Provider>;
}

export function useMe(): MeContextValue {
  const ctx = useContext(MeContext);
  if (!ctx) throw new Error("useMe must be used inside <MeProvider>");
  return ctx;
}
