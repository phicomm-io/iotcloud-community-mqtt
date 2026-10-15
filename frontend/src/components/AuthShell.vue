<script setup>
import AppIcon from './AppIcon.vue';
import ThemeToggle from './ThemeToggle.vue';
</script>
<template>
 <main class="auth-page">
  <header class="auth-topbar"><RouterLink to="/login" class="brand"><span class="brand-mark"><AppIcon name="plug" :size="23" /></span><span>Phicomm<span class="brand-subtitle">TMCP</span></span></RouterLink><ThemeToggle /></header>
  <div class="auth-layout">
   <section class="account-page surface"><slot /></section>
  </div>
  <footer class="auth-footer">phicomm-community-mqtt <span>支持 TC1 · M1 · A1</span></footer>
 </main>
</template>
