export const trAuth = {
  'auth.email': 'E-posta',
  'auth.password': 'Şifre',
  'auth.showPassword': 'Şifreyi göster',
  'auth.hidePassword': 'Şifreyi gizle',
  'auth.passwordHint': 'En az 8 karakter',
  'auth.tagline': 'BIST, ABD hisseleri, TEFAS fonları, kripto ve altın için portföy takibi',
  'auth.login.title': 'Giriş yap',
  'auth.login.submit': 'Giriş yap',
  'auth.login.noAccount': 'Hesabın yok mu? Kayıt ol',
  'auth.login.sessionExpired': 'Oturumunun süresi doldu, tekrar giriş yap',
  // No self-service reset until the app can send email (Phase 6). Until then
  // only the operator can set a password, so point the user at them instead
  // of at a link that does not exist.
  'auth.login.forgotPassword':
    'Şifreni unuttuysan kendin sıfırlayamazsın. Bu uygulamayı çalıştıran kişiye ulaş, şifreni sıfırlayabilir.',
  'auth.register.title': 'Kayıt ol',
  'auth.register.submit': 'Kayıt ol',
  'auth.register.hasAccount': 'Zaten hesabın var mı? Giriş yap',
  'auth.register.done': 'Kayıt tamamlandı, giriş yap',
  'auth.register.closed': 'Kayıt kapalı, yöneticine başvur.',
  'setup.title': 'Kurulum',
  'setup.token': 'Kurulum anahtarı',
  'setup.tokenHint': 'Kurulum anahtarı sunucu kaydında yazılıdır',
  'setup.existingHint': 'Bu e-posta zaten kayıtlıysa, şifre doğruysa o hesap yönetici olur',
  'setup.passwordAgain': 'Şifre (tekrar)',
  'setup.passwordMismatch': 'Şifreler eşleşmiyor.',
  'setup.submit': 'Yönetici hesabını oluştur',
  'setup.step': 'Adım {step} / 2',
  'setup.configure.intro':
    'Bu adım isteğe bağlı, her değişiklik hemen kaydedilir. ABD hisse fiyatları için Finnhub ve Tiingo anahtarları gerekir. Anahtarları daha sonra Yönetim sayfasından da ekleyebilirsin.',
  'setup.configure.skip': 'Şimdilik atla',
  'setup.configure.finish': 'Bitir',
  'auth.logout': 'Çıkış yap',
  'session.loading': 'Oturum yükleniyor...',
} as const
