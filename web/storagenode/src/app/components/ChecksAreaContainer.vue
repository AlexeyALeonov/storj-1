// Copyright (C) 2019 Storj Labs, Inc.
// See LICENSE for copying information.

<template>
    <div class="checks-area-container">
        <div class="checks-area-container__header">
            <p class="checks-area-container__header__title">{{label}}</p>
            <InfoComponent :text="infoText" is-extra-padding="true" is-custom-position="true">
                <div>
                    <svg width="18" height="18" viewBox="0 0 18 18" fill="none" xmlns="http://www.w3.org/2000/svg" alt="info image">
                        <rect width="18" height="18" rx="9" fill="#5A667C"/>
                        <path d="M8.99928 8.00325C8.44956 8.00325 7.99979 8.48247 7.99979 9.06819L7.99979 13.3351C7.99979 13.3883 8.00312 13.4451 8.00978 13.4984C8.08308 14.006 8.49953 14.4 8.99928 14.4C9.54901 14.4 9.99878 13.9208 9.99878 13.3351L9.99878 9.07174C9.99878 8.48247 9.54901 8.00325 8.99928 8.00325Z" fill="white"/>
                        <path d="M8.99988 6.96423C9.77415 6.96423 10.3992 6.33921 10.3992 5.56494C10.3992 4.79066 9.77415 4.16564 8.99988 4.16564C8.22561 4.16564 7.60059 4.79066 7.60059 5.56494C7.59748 6.33921 8.2225 6.96423 8.99988 6.96423Z" fill="white"/>
                    </svg>
                </div>
            </InfoComponent>
        </div>
        <p class="checks-area-container__amount"><b>{{value}}%</b></p>
    </div>
</template>

<script lang="ts">
import { Component, Prop, Vue } from 'vue-property-decorator';

import InfoComponent from '@/app/components/InfoComponent.vue';

@Component ({
    components: {
        InfoComponent,
    },
})
export default class ChecksAreaContainer extends Vue {
    @Prop({default: ''})
    private readonly label: string;
    @Prop({default: ''})
    private readonly amount: number;
    @Prop({default: ''})
    private readonly infoText: string;

    public get value(): string {
        return this.amount.toFixed(1);
    }
}
</script>

<style lang="scss">
    .checks-area-container {
        width: 325px;
        height: 70px;
        background-color: #FFFFFF;
        border: 1px solid #E9EFF4;
        border-radius: 11px;
        padding: 34px 36px 39px 39px;
        margin-bottom: 32px;
        position: relative;

        &__header {
            display: flex;
            align-items: center;

            &__title {
                font-size: 14px;
                color: #586C86;
            }

            p {
                margin-right: 5px;
            }

            svg {
                margin-top: 3px;

                &:hover {
                    cursor: pointer;

                    rect {
                        fill: #A5C7EF;
                    }
                }
            }
        }

        &__amount {
            font-size: 32px;
            line-height: 57px;
            color: #535F77;
        }
    }
</style>
