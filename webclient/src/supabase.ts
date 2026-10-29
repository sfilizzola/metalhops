import { createClient } from "@supabase/supabase-js";

const url = import.meta.env.VITE_SUPABASE_URL;
const key = import.meta.env.VITE_SUPABASE_PUBLISHABLE_KEY;

// supabase-js v2 defaults (persistSession, autoRefreshToken, detectSessionInUrl all true) are kept.
export const supabase = url && key ? createClient(url, key) : null;

export const apiBaseUrl: string = import.meta.env.VITE_API_BASE_URL || "http://localhost:8080";
