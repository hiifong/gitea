<script lang='ts'>
import {createApp} from 'vue';
import {SvgIcon} from '../svg.ts';

enum visibility {
  public = 0,
  limited = 1,
  private = 2,
}

type user = {
  name: string,
  avatar: string,
  homeLink: string,
  visibility: visibility | null,
  isAdmin: boolean,
}

const sfc = {
  name: 'ProfileOrgList',
  components: {SvgIcon},
  data() {
    const a = 5;
    const list: Array<user> = [];
    return {
      a,
      orgList: list,
      isLoading: false,
    };
  },
  methods: {
    async getOrgList() {
      this.isLoading = true;

      console.log('=================>>>>>>>>>>>>>>>');

      for (let i = 0; i < 100; i++) {
        this.orgList.push({
          name: 'aaa',
          avatar: '/avatars/47bce5c74f589f4867dbd57e9ca9f808',
          homeLink: '/aaa',
          visibility: 1,
          isAdmin: false,
        });
      }

      console.log('========> response: ', this.orgList);
    },

    bindAvatar(avatar: string, size: number = 56) {
      return `${avatar}?size=${size}`;
    },
  },

  mounted() {
    this.getOrgList();
  },
};

export default sfc;

export function initProfileOrgList() {
  const el = document.querySelector('.user-orgs');
  if (el) {
    createApp(sfc).mount(el);
  }
}
</script>

<template>
  <li v-for="(org, index) in orgList" :key="index">
    <a :href="org.homeLink" :data-tooltip-content="org.name" :aria-label="org.name">
      <img
        loading="lazy" class="ui avatar tw-align-middle" :src="bindAvatar(org.avatar)" :title="org.name"
        width="28" height="28"
      >
    </a>
  </li>
  <div v-if="orgList.length > 50" class="divider tw-my-0"/>
  <div v-if="orgList.length > 50" class="center">
    <div class="tw-text-center">
      <div class="ui borderless pagination menu narrow tw-my-2">
        <a
          class="item navigation tw-py-1" title=""
        >
          <svg-icon name="gitea-double-chevron-left" :size="16" class-name="tw-mr-1"/>
        </a>
        <a
          class="item navigation tw-py-1" title="previousPage"
        >
          <svg-icon name="octicon-chevron-left" :size="16" clsas-name="tw-mr-1"/>
        </a>
        <a class="active item tw-py-1">1</a>
        <a
          class="item navigation" title="nextPage"
        >
          <svg-icon name="octicon-chevron-right" :size="16" class-name="tw-ml-1"/>
        </a>
        <a
          class="item navigation tw-py-1" title="lastPage"
        >
          <svg-icon name="gitea-double-chevron-right" :size="16" class-name="tw-ml-1"/>
        </a>
      </div>
    </div>
  </div>
</template>

<style scoped>
.center {
  width: 100%;
  display: flex;
  justify-content: center;
}
</style>
