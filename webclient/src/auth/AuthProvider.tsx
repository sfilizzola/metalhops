import { createContext, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import type { Session } from "@supabase/supabase-js";
import { supabase } from "../supabase";

// undefined until Supabase reports the initial session, so the login form never flashes.
const SessionContext = createContext<Session | null | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null | undefined>(undefined);

  useEffect(() => {
    if (!supabase) return;
    const { data } = supabase.auth.onAuthStateChange((_event, s) => setSession(s));
    return () => data.subscription.unsubscribe();
  }, []);

  return <SessionContext.Provider value={session}>{children}</SessionContext.Provider>;
}

export function useSession(): Session | null | undefined {
  return useContext(SessionContext);
}
