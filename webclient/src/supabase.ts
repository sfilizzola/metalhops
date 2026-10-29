import { createClient } from "@supabase/supabase-js";

const url = import.meta.env.VITE_SUPABASE_URL;
const key = import.meta.env.VITE_SUPABASE_PUBLISHABLE_KEY;

// supabase-js v2 defaults (persistSession, autoRefreshToken, detectSessionInUrl all true) are kept.
export const supabase = url && key ? createClient(url, key) : null;

// The localhost default applies only under `npm run dev`; builds must set VITE_API_BASE_URL.
export const apiBaseUrl: string = import.meta.env.VITE_API_BASE_URL || (import.meta.env.DEV ? "http://localhost:8080" : "");
