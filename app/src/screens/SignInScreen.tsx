import { useEffect, useRef, useState } from 'react';
import { KeyboardAvoidingView, Platform, Pressable, StyleSheet, Text, TextInput, View } from 'react-native';
import { ApiError, apiJson } from '../api/client';
import { useAuth } from '../auth/AuthContext';
import { Button, TextField } from '../components/ui';
import { Logo } from '../components/Logo';
import { Theme, useThemedStyles } from '../theme';

const RESEND_AFTER_S = 60;

// One screen for both signing in and signing up: email, then the emailed
// code, then a name only if this email has no account yet.
export default function SignInScreen() {
  const { requestCode, verify } = useAuth();
  const styles = useThemedStyles(makeStyles);
  const [step, setStep] = useState<'email' | 'code' | 'name'>('email');
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [resendIn, setResendIn] = useState(0);
  // The server's invite code for new accounts (SUPERCALENDAR_INVITE_CODE):
  // the field shows if /config says there is one, or a sign-in asks for it.
  const [needsInvite, setNeedsInvite] = useState(false);
  const [invite, setInvite] = useState('');

  useEffect(() => {
    apiJson<{ inviteCode?: boolean }>('/config')
      .then((c) => c.inviteCode && setNeedsInvite(true))
      .catch(() => {});
  }, []);
  const codeRef = useRef<TextInput>(null);
  const emailRef = useRef<TextInput>(null);

  useEffect(() => {
    if (resendIn <= 0) return;
    const t = setTimeout(() => setResendIn((s) => s - 1), 1000);
    return () => clearTimeout(t);
  }, [resendIn]);

  const run = async (fn: () => Promise<void>) => {
    setError(null);
    setBusy(true);
    try {
      await fn();
    } catch (e: any) {
      setError(e.message ?? 'Something went wrong');
    } finally {
      setBusy(false);
    }
  };

  const sendCode = () =>
    run(async () => {
      try {
        await requestCode(email.trim().toLowerCase(), invite.trim() || undefined);
      } catch (e) {
        if (e instanceof ApiError && e.body?.needsInvite) setNeedsInvite(true);
        throw e;
      }
      setStep('code');
      setCode('');
      setResendIn(RESEND_AFTER_S);
      setTimeout(() => codeRef.current?.focus(), 100);
    });

  const submitCode = (value = code) =>
    run(async () => {
      const result = await verify(email.trim().toLowerCase(), value);
      if (result === 'needsName') setStep('name');
    });

  const submitName = () =>
    run(async () => {
      await verify(email.trim().toLowerCase(), code, name.trim());
    });

  const onCodeChange = (value: string) => {
    const digits = value.replace(/\D/g, '').slice(0, 6);
    setCode(digits);
    if (digits.length === 6) submitCode(digits);
  };

  return (
    <KeyboardAvoidingView style={styles.root} behavior={Platform.OS === 'ios' ? 'padding' : undefined}>
      <View style={{ alignItems: 'center', marginBottom: 12 }}>
        <Logo size={88} />
      </View>
      <Text style={styles.title}>Super Calendar</Text>

      {step === 'email' && (
        <>
          <Text style={styles.subtitle}>Enter your email and we'll send you a code.</Text>
          <TextField
            ref={emailRef}
            autoFocus
            placeholder="you@example.com"
            autoCapitalize="none"
            autoComplete="email"
            keyboardType="email-address"
            value={email}
            onChangeText={setEmail}
            onSubmitEditing={sendCode}
            returnKeyType="send"
          />
          {needsInvite && (
            <>
              <TextField
                placeholder="Invite code"
                autoCapitalize="none"
                autoCorrect={false}
                value={invite}
                onChangeText={setInvite}
                onSubmitEditing={sendCode}
                returnKeyType="send"
                style={styles.gap}
              />
              <Text style={styles.hint}>Only needed for a new account. Super Calendar is invite-only.</Text>
            </>
          )}
          <Button label="Send code" onPress={sendCode} loading={busy} disabled={!email.includes('@')} style={styles.gap} />
        </>
      )}

      {step === 'code' && (
        <>
          <Text style={styles.subtitle}>Enter the 6-digit code we sent to {email.trim()}.</Text>
          <TextField
            ref={codeRef}
            placeholder="123456"
            keyboardType="number-pad"
            autoComplete="one-time-code"
            textContentType="oneTimeCode"
            maxLength={6}
            value={code}
            onChangeText={onCodeChange}
            style={styles.codeInput}
          />
          <Button label="Continue" onPress={() => submitCode()} loading={busy} disabled={code.length !== 6} style={styles.gap} />
          <View style={styles.links}>
            <Pressable onPress={() => setStep('email')} hitSlop={8}>
              <Text style={styles.link}>Change email</Text>
            </Pressable>
            <Pressable onPress={sendCode} disabled={resendIn > 0 || busy} hitSlop={8}>
              <Text style={[styles.link, resendIn > 0 && styles.linkDisabled]}>
                {resendIn > 0 ? `Resend in ${resendIn}s` : 'Resend code'}
              </Text>
            </Pressable>
          </View>
        </>
      )}

      {step === 'name' && (
        <>
          <Text style={styles.subtitle}>Welcome! What should we call you?</Text>
          <TextField
            placeholder="Your name"
            autoCapitalize="words"
            autoComplete="name"
            autoFocus
            value={name}
            onChangeText={setName}
            onSubmitEditing={submitName}
            maxLength={40}
          />
          <Button label="Get started" onPress={submitName} loading={busy} disabled={!name.trim()} style={styles.gap} />
        </>
      )}

      {error && <Text style={styles.error}>{error}</Text>}
    </KeyboardAvoidingView>
  );
}

const makeStyles = (theme: Theme) =>
  StyleSheet.create({
    root: { flex: 1, backgroundColor: theme.bg, padding: 24, justifyContent: 'center' },
    title: { color: theme.text, fontSize: 30, fontWeight: '700', marginBottom: 8 },
    subtitle: { color: theme.textMuted, fontSize: 15, marginBottom: 24, lineHeight: 21 },
    gap: { marginTop: 16 },
    hint: { color: theme.textMuted, fontSize: 13, marginTop: 6 },
    codeInput: { fontSize: 24, letterSpacing: 8, textAlign: 'center' },
    links: { flexDirection: 'row', justifyContent: 'space-between', marginTop: 20 },
    link: { color: theme.primary, fontWeight: '600' },
    linkDisabled: { color: theme.textMuted },
    error: { color: theme.danger, marginTop: 16 },
  });
